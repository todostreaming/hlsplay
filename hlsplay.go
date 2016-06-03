package hlsplay

import (
	"bufio"
	"fmt"
	"github.com/isaacml/cmdline"
	"github.com/todostreaming/cola"
	"github.com/todostreaming/m3u8pls"
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

const (
	fiforoot     = "/var/segments/" // /tmp/
	queuetimeout = 60               // creamos una cola con un timeout de 2 minutos = 120 secs
)

var (
	Warning *log.Logger
	fw      *os.File //FIFO1 File descriptor
)

func init() {
	exec.Command("/bin/sh", "-c", "rm -f "+fiforoot+"fifo*").Run()
	syscall.Mkfifo(fiforoot+"fifo1", 0666)
	syscall.Mkfifo(fiforoot+"fifo2", 0666)
	_, err := os.Stat(fiforoot + "fifo1")
	if err != nil {
		log.Fatal("hlsplay-init() fifo1")
	}
	_, err = os.Stat(fiforoot + "fifo2")
	if err != nil {
		log.Fatal("hlsplay-init() fifo2")
	}
	Warning = log.New(os.Stderr, "\n\n[WARNING]: ", log.Ldate|log.Ltime|log.Lshortfile)
}

type Status struct {
	Playing bool // omxplayer esta reproduciendo
	Running bool // proceso completo funcionando
	Volume  int  // dB
	Numsegs int
	Kbps    int    // download kbps speed
	OMXStat string // log del omxplayer
	OldBuf  float64
	Buf     float64
}

type Segmento struct {
	segname string
	segdur  float64
}

type HLSPlay struct {
	cmdomx        string
	exe           *cmdline.Exec
	exe2          *cmdline.Exec
	mediawriter   *bufio.Writer     // por aqui puedo enviar caracteres al omxplayer
	settings      map[string]string // read-only map
	duration      []float64         // matrices de duracion en segundos de los segmentos play?.ts
	downloaddir   string            // directorio RAMdisk donde se guardan los ficheros bajados del server y listos para reproducir
	m3u8          string
	playing       bool       // omxplayer esta reproduciendo
	restamping    bool       // ffmpeg esta reestampando
	downloading   bool       // esta bajando segmentos
	running       bool       // proceso completo funcionando
	volume        int        // dB
	mu_seg        sync.Mutex // Mutex para las variables internas del objeto HLSPlay
	numsegs       int
	lastTargetdur float64
	lastMediaseq  int64
	lastIndex     int              // index del segmento donde toca copiar download.ts  entre 0 y numsegs-1
	lastPlay      int              // index del segmento que se envió al secuenciador desde el director
	lastkbps      int              // download kbps speed
	omxstat       string           // log del omxplayer
	oldbuf, buf   float64          // buffer anterior y actual del omx
	m3u8pls       *m3u8pls.M3U8pls // parser M3U8
	cola          *cola.Cola       // cola con los segments/dur para bajar
	mu_play       []sync.Mutex     // Mutex para la escritura/lectura de segmentos *.ts cíclicos
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
	hls.lastTargetdur = 0.0
	hls.lastMediaseq = 0
	hls.lastIndex = 0
	hls.lastPlay = 0
	hls.lastkbps = 0
	hls.oldbuf = 0.0
	hls.buf = 0.0
	hls.m3u8pls = m3u8pls.M3U8playlist(hls.m3u8)
	hls.cola = cola.CreateQueue(queuetimeout)
	hls.omxstat = ""
	// calculamos los segmentos máximos que caben
	ramdisk, ok := hls.settings["ramdisk"] // ramdisk in MBs
	if !ok {
		ramdisk = "128" // 128 MBs by default
	}
	numsegs := (2 * toInt(ramdisk) / 15) - 4
	hls.numsegs = int(numsegs)
	hls.mu_play = make([]sync.Mutex, hls.numsegs) // segmentos de maximo 12 segundos a 5 Mbps
	hls.duration = make([]float64, hls.numsegs)

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
	exec.Command("/bin/sh", "-c", "rm -f "+h.downloaddir+"*.ts").Run() // equivale a rm -f /var/segments/*.ts
	h.running = true                                                   // comienza a correr
	h.mu_seg.Unlock()

	fw, err = os.OpenFile(fiforoot+"fifo1", os.O_WRONLY|os.O_TRUNC, 0666) /// |os.O_CREATE|os.O_APPEND (O_WRONLY|O_CREAT|O_TRUNC)
	if err != nil {
		Warning.Fatalln(err)
	}
	go h.command1(ch)
	go h.command2(ch)
	go h.m3u8parser()
	go h.downloader() // bajando a su bola sin parar
	go h.director()   // envia segmentos al secuenciador cuando s.playing && s.restamping

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
	h.playing = false
	h.restamping = false
	h.downloading = false
	h.running = false
	h.lastTargetdur = 0.0
	h.lastMediaseq = 0
	h.lastIndex = 0
	h.lastPlay = 0
	h.lastkbps = 0
	h.oldbuf = 0.0
	h.buf = 0.0
	h.cola = cola.CreateQueue(queuetimeout)
	h.duration = make([]float64, h.numsegs)
	h.omxstat = ""
	killall("omxplayer.bin")
	h.exe.Stop()
	err = h.exe2.Stop()
	if err != nil {
		err = fmt.Errorf("hlsplay: STOP_ERROR")
	}

	return err
}

// you dont need to call this func less than secondly
func (h *HLSPlay) Status() *Status {
	var st Status

	h.mu_seg.Lock()
	defer h.mu_seg.Unlock()

	st.Playing = h.playing
	st.Running = h.running
	st.Volume = h.volume
	st.Numsegs = h.numsegs
	st.Kbps = h.lastkbps
	st.OMXStat = h.omxstat
	st.OldBuf = h.oldbuf
	st.Buf = h.buf

	return &st
}

func (h *HLSPlay) Volume(up bool) {
	h.mu_seg.Lock()
	defer h.mu_seg.Unlock()
	if up {
		if h.volume < 12 {
			h.mediawriter.WriteByte('+')
			h.mediawriter.Flush()
		}
	} else {
		if h.volume > -12 {
			h.mediawriter.WriteByte('-')
			h.mediawriter.Flush()
		}
	}
}

func (h *HLSPlay) m3u8parser() {
	for {
		h.cola.Keeping()
		h.m3u8pls.Parse()  // bajamos y parseamos la url m3u8 HLS a reproducir
		if !h.m3u8pls.Ok { // m3u8 no accesible o explotable
			time.Sleep(2 * time.Second)
			continue
		}
		h.mu_seg.Lock()
		if !h.running {
			h.mu_seg.Unlock()
			break
		}
		if h.m3u8pls.Mediaseq == h.lastMediaseq { // no ha cambiado el m3u8 aún
			h.mu_seg.Unlock()
			time.Sleep(time.Duration(h.m3u8pls.Targetdur/2.0) * time.Second)
			continue
		}
		h.lastMediaseq = h.m3u8pls.Mediaseq
		h.lastTargetdur = h.m3u8pls.Targetdur
		for k, v := range h.m3u8pls.Segment { // segmento
			h.cola.Add(v, h.m3u8pls.Duration[k])
		}
		h.mu_seg.Unlock()
		////h.cola.Print()

		time.Sleep(time.Duration(h.m3u8pls.Targetdur) * time.Second)
	}
}

func (h *HLSPlay) downloader() {
	started := true
	for {
		h.mu_seg.Lock()
		if !h.running {
			h.mu_seg.Unlock()
			break
		}
		h.mu_seg.Unlock()
		if h.cola.Len() < 1 {
			time.Sleep(1 * time.Second)
			continue
		}
		segname, segdur := h.cola.Next()
		if segname == "" && segdur == 0.0 {
			time.Sleep(1 * time.Second)
			continue
		}
		os.Remove(h.downloaddir + "download.ts")
		syscall.Sync()
		kbps, ok := download(h.downloaddir+"download.ts", segname, segdur)
		if !ok {
			runtime.Gosched()
			continue
		}

		h.mu_seg.Lock()
		h.lastkbps = kbps
		h.mu_seg.Unlock()
		if started {
			started = false
			// copiar numsegs veces el segmento download.ts
			for i := 0; i < h.numsegs; i++ {
				h.mu_seg.Lock()
				h.duration[i] = segdur
				h.mu_seg.Unlock()
				cp := fmt.Sprintf("cp -f %sdownload.ts %splay%d.ts", h.downloaddir, h.downloaddir, i)
				////fmt.Printf("[downloader] - 4 => %s\n",cp)
				h.mu_play[i].Lock()
				exec.Command("/bin/sh", "-c", cp).Run()
				syscall.Sync()
				h.mu_play[i].Unlock()
			}
		} else {
			// copiar solo una vez donde corresponde download.ts
			h.mu_seg.Lock()
			h.lastIndex++
			if h.lastIndex >= h.numsegs {
				h.lastIndex = 0
			}
			i := h.lastIndex
			h.duration[i] = segdur
			h.mu_seg.Unlock()

			cp := fmt.Sprintf("cp -f %sdownload.ts %splay%d.ts", h.downloaddir, h.downloaddir, i)
			////fmt.Printf("[downloader] - 5 => %s\n",cp)
			h.mu_play[i].Lock()
			exec.Command("/bin/sh", "-c", cp).Run()
			syscall.Sync()
			h.mu_play[i].Unlock()
		}
		runtime.Gosched()
	}
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
		h.cmdomx = fmt.Sprintf("/usr/bin/omxplayer -s -o both --vol %d --hw%s --layer 100 --no-osd -b %sfifo2", 100*vol, overscan, fiforoot)
		////fmt.Println(h.cmdomx)
		h.exe = cmdline.Cmdline(h.cmdomx)
		lectura, err := h.exe.StderrPipe()
		if err != nil {
			Warning.Println(err)
		}
		mReader := bufio.NewReader(lectura)

		stdinWrite, err := h.exe.StdinPipe()
		if err != nil {
			Warning.Println(err)
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
					fmt.Println("\nTimeout omxplayer !!!")
					fmt.Fprintln(os.Stderr, "\nTimeout omxplayer !!!")
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
				fmt.Println("\nFin del omxplayer !!!")
				fmt.Fprintln(os.Stderr, "\nFin del omxplayer !!!")
				break
			}
			line = strings.TrimRight(line, "\n")
			if strings.Contains(line, "Comenzando...") {
				////fmt.Println("[omx]", "Ready...")
				ch <- 1 // enviamos mensaje de que omx esta listo para que ffmpeg arranque
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
				var hh, mm, ss, drops, cached, vol int
				var playbuf float64

				h.mu_seg.Lock()
				h.oldbuf = h.buf
				fmt.Sscanf(line, "Time: %d:%d:%d Drops: %d PlayBuf: %fs Cached: %dk Vol: %d dB", &hh, &mm, &ss, &drops, &playbuf, &cached, &vol)
				h.buf = playbuf
				h.omxstat = line
				h.mu_seg.Unlock()
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
	for {
		cmd2 := fmt.Sprintf("/usr/bin/ffmpeg -y -f mpegts -i %sfifo1 -f mpegts -acodec copy -vcodec copy %sfifo2", fiforoot, fiforoot)
		////fmt.Println(cmd2)
		h.exe2 = cmdline.Cmdline(cmd2)
		lectura, err := h.exe2.StderrPipe()
		if err != nil {
			Warning.Println(err)
		}
		mReader := bufio.NewReader(lectura)
		<-ch // omx ya esta listo, vamos a arrancar ffmpeg
		h.exe2.Start()

		for { // bucle de reproduccion normal
			line, err := mReader.ReadString('\n')
			if err != nil {
				fw.Close() // closes FIFO1
				h.mu_seg.Lock()
				h.restamping = false
				h.mu_seg.Unlock()
				fmt.Fprintln(os.Stderr, "\nFin del ffmpeg !!!")
				fmt.Println("\nFin del ffmpeg !!!")
				break
			}
			line = strings.TrimRight(line, "\n")
			if strings.Contains(line, "libpostproc") {
				////fmt.Println("[ffmpeg]", "Ready...")
				h.mu_seg.Lock()
				h.restamping = true
				h.mu_seg.Unlock()
				fw, err = os.OpenFile(fiforoot+"fifo1", os.O_WRONLY|os.O_TRUNC, 0666) /// |os.O_CREATE|os.O_APPEND (O_WRONLY|O_CREAT|O_TRUNC)
				if err != nil {
					Warning.Fatalln(err)
				}
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
func (h *HLSPlay) secuenciador(file string, indexPlay int) error {

	h.mu_play[indexPlay].Lock()
	fr, err := os.Open(file) // read-only
	if err != nil {
		Warning.Println(err)
		return err
	}
	if _, err := io.Copy(fw, fr); err == nil { // possible issue when fw is closed
		////fmt.Printf("[secuenciador] (%s) Copiados %d bytes\n", file, n) // copia perfecta sin fallos
	} else {
		Warning.Println(err) // no salimos en caso de error de copia en algun momento
	}
	fr.Close()
	h.mu_play[indexPlay].Unlock()

	return err
}

func (h *HLSPlay) director() {
	started := true
	for {
		if started {
			started = false
			time.Sleep(12 * time.Second)
		}

		h.mu_seg.Lock()
		if !h.running {
			h.mu_seg.Unlock()
			break
		}
		indexplay := h.lastPlay
		h.mu_seg.Unlock()

		file := fmt.Sprintf("%splay%d.ts", h.downloaddir, indexplay)
		////fmt.Printf("[director] Play %s\n",file)
		err := h.secuenciador(file, indexplay)
		if err != nil { // si pasa por aqui se supone que el FIFO1 esta muerto, y reintenta hasta que reviva cada segundo
			Warning.Println(err)
			time.Sleep(1 * time.Second)
			continue
		}

		h.mu_seg.Lock()
		h.lastPlay++
		if h.lastPlay >= h.numsegs {
			h.lastPlay = 0
		}
		espera := time.Duration(h.duration[indexplay] * 1000.0 / 2.0)
		h.mu_seg.Unlock()

		time.Sleep(espera * time.Millisecond) // espera al tirar el tronco en milisegundos justo la mitad de la duracion del .ts
		for {
			h.mu_seg.Lock()
			if (h.oldbuf > h.buf) && (h.buf < 6.0) {
				h.mu_seg.Unlock()
				break
			}
			h.mu_seg.Unlock()
			time.Sleep(100 * time.Millisecond) // esperamos 100 ms para revisar de nuevo la tendendia del playbuffer de omx
		}

	}
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

// baja un segmento al fichero download y lo reintenta 3 veces con un timeout 2 * segdur
// download es la direccion absoluta del fichero donde bajarlo
// segname es la URL completa del fichero a bajar
// segdur es la duración media del fichero (importante para el timeout)
// devuelve kbps de download y ok
func download(download, segname string, segdur float64) (int, bool) {
	var bytes int64
	var downloaded, downloadedok bool
	var kbps int
	var downloading bool

	cmd := fmt.Sprintf("/usr/bin/wget -t 3 --limit-rate=625k -S -O %s %s", download, segname)
	////fmt.Println(cmd)
	exe := cmdline.Cmdline(cmd)

	lectura, err := exe.StderrPipe()
	if err != nil {
		Warning.Println(err)
	}
	mReader := bufio.NewReader(lectura)
	tiempo := time.Now().Unix()
	go func() {
		for {
			if (time.Now().Unix()-tiempo) > int64(segdur) && downloading {
				exe.Stop()
				////fmt.Println("[download] WGET matado supera los XXX segundos !!!!")
				break
			}
			time.Sleep(1 * time.Second)
		}
	}()
	downloading = true
	ns := time.Now().UnixNano()
	exe.Start()
	for { // bucle de reproduccion normal
		line, err := mReader.ReadString('\n')
		if err != nil {
			////fmt.Println("Fin del wget !!!")
			break
		}
		line = strings.TrimRight(line, "\n")
		if strings.Contains(line, "HTTP/1.1 200 OK") {
			////fmt.Println("[downloader] Downloaded OK")
			downloaded = true
		}
		if strings.Contains(line, "Content-Length:") { //   Content-Length: 549252
			line = strings.Trim(line, " ")
			fmt.Sscanf(line, "Content-Length: %d", &bytes)
		}
		////fmt.Printf("[wget] %s\n", line) //==>
	}
	exe.Stop()
	downloading = false
	ns = time.Now().UnixNano() - ns

	if downloaded {
		// comprobar que el fichero se ha bajado correctamente
		fileinfo, err := os.Stat(download) // fileinfo.Size()
		if err != nil {
			downloadedok = false
			Warning.Println(err)
		}
		filesize := fileinfo.Size()
		if filesize == int64(bytes) {
			downloadedok = true
		} else {
			downloadedok = false
		}
		if ns != 0 { // evitar un 0 divisor
			kbps = int(filesize * 8.0 * 1e9 / ns / 1000.0)
		}
	}

	return kbps, downloadedok
}
