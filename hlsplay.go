package hlsplay

import (
	"bufio"
	"fmt"
	"github.com/isaacml/cmdline"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func init() {
	syscall.Mkfifo("/tmp/fifo1", 0666)
	syscall.Mkfifo("/tmp/fifo2", 0666)
}

type HLSPlay struct {
	cmdomx      string
	exe         *cmdline.Exec
	exe2        *cmdline.Exec
	mediawriter *bufio.Writer     // por aqui puedo enviar caracteres al omxplayer
	settings    map[string]string // read-only map
	downloaddir string            // directorio RAMdisk donde se guardan los ficheros bajados del server y listos para reproducir
	m3u8        string
	playing     bool         // omxplayer esta reproduciendo
	restamping  bool         // ffmpeg esta reestampando
	downloading bool         // esta bajando segmentos
	running     bool         // proceso completo funcionando
	semaforo    string       // R(red), Y(yellow), G(green) download speed
	volume      int          // dB
	mu_seg      sync.Mutex   // Mutex para las variables internas del objeto HLSPlay
	mu_play     []sync.Mutex // Mutex para la escritura/lectura de segmentos *.ts cíclicos
}

func HLSPlayer(m3u8, downloaddir string, settings map[string]string) *HLSPlay {
	hls := &HLSPlay{}
	hls.mu_seg.Lock()
	defer hls.mu_seg.Unlock()
	hls.settings = settings
	hls.downloaddir = downloaddir
	hls.m3u8 = m3u8
	hls.playing = false
	hls.restamping = false
	hls.downloading = false
	hls.running = false
	hls.semaforo = "G"                   // comenzamos en verde
	hls.mu_play = make([]sync.Mutex, 30) // 30 segmentos ciclados (??? calcular antes ???)

	return hls
}

func (h *HLSPlay) Run() error {
	var err error
	ch := make(chan int)

	h.mu_seg.Lock()
	if h.running { // ya esta corriendo
		h.mu_seg.Unlock()
		return fmt.Errorf("hlsplay: ALREADY_RUNNING_ERROR")
	}
	// borrar la base de datos de RAM y los ficheros *.ts
	exec.Command("/bin/sh", "-c", "rm -f "+h.downloaddir+"*.ts").Run()   // equivale a rm -f /var/segments/*.ts
	exec.Command("/bin/sh", "-c", "rm -f "+h.downloaddir+"*.m3u8").Run() // equivale a rm -f /var/segments/*.m3u8
	h.running = true                                                     // comienza a correr
	h.mu_seg.Unlock()

	go h.command1(ch)
	go h.command2(ch)
	//	go h.downloader() // bajando a su bola sin parar lo que el director le diga de donde bajarlo (tv_id, mac, ip_download)
	//	go h.director()   // envia segmentos al secuenciador cuando s.playing && s.restamping

	return err
}

func (h *HLSPlay) Stop() error {
	var err error

	h.mu_seg.Lock()
	defer h.mu_seg.Unlock()
	if !h.running {
		return fmt.Errorf("hlsplay: ALREADY_STOPPED_ERROR")
	}
	h.running = false
	killall("omxplayer.bin")
	h.exe.Stop()
	err = h.exe2.Stop()
	if err != nil {
		err = fmt.Errorf("hlsplay: STOP_ERROR")
	}

	return err
}

func (h *HLSPlay) command1(ch chan int) { // omxplayer
	var tiempo int64
	for {
		var overscan string
		h.mu_seg.Lock()
		if h.settings["overscan"] == "1" {
			overscan = fmt.Sprintf(" --win %s,%s,%s,%s", h.settings["x0"], h.settings["y0"], h.settings["x1"], h.settings["y1"])
		}
		vol := toInt(h.settings["vol"])
		// creamos el cmdomx
		// /usr/bin/omxplayer -s -o both --vol 100 --hw --win '0 0 719 575' --no-osd -b /tmp/fifo2
		h.cmdomx = fmt.Sprintf("/usr/bin/omxplayer -s -o both --vol %d --hw%s --layer 100 --no-osd -b --live --threshold 1.0 /tmp/fifo2", 100*vol, overscan)
		h.exe = cmdline.Cmdline(h.cmdomx)
		lectura, err := h.exe.StderrPipe()
		if err != nil {
			fmt.Println(err)
		}
		mReader := bufio.NewReader(lectura)

		stdinWrite, err := h.exe.StdinPipe()
		if err != nil {
			fmt.Println(err)
		}
		h.mediawriter = bufio.NewWriter(stdinWrite)
		h.mu_seg.Unlock()
		tiempo = time.Now().Unix()
		go func() {
			for {
				if (time.Now().Unix() - tiempo) > 10 {
					h.mu_seg.Lock()
					h.restamping = false
					h.playing = false
					h.mu_seg.Unlock()
					killall("omxplayer.bin")
					h.exe.Stop()
					break
				}
				time.Sleep(1 * time.Second)
			}
		}()
		h.exe.Start()

		for { // bucle de reproduccion normal
			tiempo = time.Now().Unix() //; time.Sleep(5 * time.Second)
			line, err := mReader.ReadString('\n')
			if err != nil {
				h.mu_seg.Lock()
				h.playing = false
				h.mu_seg.Unlock()
				////fmt.Println("Fin del omxplayer !!!")
				break
			}
			line = strings.TrimRight(line, "\n")
			if strings.Contains(line, "Comenzando...") {
				////fmt.Println("[omx]", "Ready...")
				ch <- 1
				h.mu_seg.Lock()
				h.playing = true
				h.mu_seg.Unlock()
			}
			if strings.Contains(line, "Current Volume:") { // Current Volume: -2 => "Current Volume: %d\n"
				var currvol int
				fmt.Sscanf(line, "Current Volume: %d", &currvol)
				h.mu_seg.Lock()
				h.settings["vol"] = fmt.Sprintf("%d", currvol)
				h.volume = currvol
				h.mu_seg.Unlock()
			}
			if strings.Contains(line, "Time:") {
				////fmt.Printf("[omx] %s\n", line)
			}
			runtime.Gosched()
		}
		killall("omxplayer.bin")
		h.exe.Stop()
		h.exe2.Stop()
		h.mu_seg.Lock()
		if !h.running {
			h.mu_seg.Unlock()
			break
		}
		h.mu_seg.Unlock()
		ch <- 1
	}
}

func (h *HLSPlay) command2(ch chan int) { // ffmpeg
	var tiempo int64
	for {
		h.exe2 = cmdline.Cmdline("/usr/bin/ffmpeg -y -f mpegts -re -i /tmp/fifo1 -f mpegts -acodec copy -vcodec copy /tmp/fifo2")
		lectura, err := h.exe2.StderrPipe()
		if err != nil {
			fmt.Println(err)
		}
		mReader := bufio.NewReader(lectura)
		tiempo = time.Now().Unix()
		go func() {
			for {
				if (time.Now().Unix() - tiempo) > 5 {
					h.mu_seg.Lock()
					h.restamping = false
					h.playing = false
					h.mu_seg.Unlock()
					h.exe2.Stop()
					break
				}
				time.Sleep(1 * time.Second)
			}
		}()
		<-ch
		h.exe2.Start()

		for { // bucle de reproduccion normal
			tiempo = time.Now().Unix()
			line, err := mReader.ReadString('\n')
			if err != nil {
				h.mu_seg.Lock()
				h.restamping = false
				h.mu_seg.Unlock()
				////fmt.Println("Fin del ffmpeg !!!")
				break
			}
			line = strings.TrimRight(line, "\n")
			if strings.Contains(line, "libpostproc") {
				////fmt.Println("[ffmpeg]", "Ready...")
				h.mu_seg.Lock()
				h.restamping = true
				h.mu_seg.Unlock()
			}
			if strings.Contains(line, "frame=") {
				////fmt.Printf("[ffmpeg] %s\n", line)
			}
			runtime.Gosched()
		}
		h.exe2.Stop()
		killall("omxplayer.bin")
		h.exe.Stop()
		h.mu_seg.Lock()
		if !h.running {
			h.mu_seg.Unlock()
			break
		}
		h.mu_seg.Unlock()
		<-ch
	}
}

// esta funcion envia los ficheros a reproducir a la cola de reproducción en el FIFO1 /tmp/fifo1
// secuencia /tmp/fifo1
func (h *HLSPlay) secuenciador(file string) {

	fw, err := os.OpenFile("/tmp/fifo1", os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		log.Fatalln(err)
	}
	defer fw.Close()

	fr, err := os.Open(file) // read-only
	if err != nil {
		log.Fatalln(err)
	}
	if n, err := io.Copy(fw, fr); err == nil {
		fmt.Printf("[secuenciador] (%s) Copiados %d bytes\n", file, n)
	} else {
		log.Println(err) // no salimos en caso de error de copia
	}
	fr.Close()

}

// killall("omxplayer omxplayer.bin ffmpeg")
func killall(list string) {
	prog := strings.Fields(list)
	for _, v := range prog {
		exec.Command("/bin/sh", "-c", "/bin/kill -9 `ps -A | /usr/bin/awk '/"+v+"/{print $1}'`").Run()
	}
}

// convierte un string numérico en un entero int
func toInt(cant string) (res int) {
	res, _ = strconv.Atoi(cant)
	return
}
