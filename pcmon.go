//go:build windows

package main

// Página 3: monitor del PC (CPU, GPU, RAM, disco, red, sistema).

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	pdh                           = syscall.NewLazyDLL("pdh.dll")
	pPdhOpenQueryW                = pdh.NewProc("PdhOpenQueryW")
	pPdhAddEnglishCounterW        = pdh.NewProc("PdhAddEnglishCounterW")
	pPdhCollectQueryData          = pdh.NewProc("PdhCollectQueryData")
	pPdhGetFormattedCounterValue  = pdh.NewProc("PdhGetFormattedCounterValue")
	pPdhGetFormattedCounterArrayW = pdh.NewProc("PdhGetFormattedCounterArrayW")
	pGlobalMemoryStatusEx         = kernel32x.NewProc("GlobalMemoryStatusEx")
	pGetDiskFreeSpaceExW          = kernel32x.NewProc("GetDiskFreeSpaceExW")
	pGetTickCount64               = kernel32x.NewProc("GetTickCount64")
	pGetSystemTimes               = kernel32x.NewProc("GetSystemTimes")
	pRegGetValueW                 = syscall.NewLazyDLL("advapi32.dll").NewProc("RegGetValueW")
)

const histLen = 60

type PCStats struct {
	CPU, CPUMHz, CPUTemp      float64 // temp < 0 = no disponible
	CPUName                   string
	GPU, GPUTemp, GPUPower    float64 // < 0 = no disponible
	GPUFan, GPUMHz            float64
	GPUName                   string
	VRAMUsed, VRAMTotal       float64 // GB
	RAMUsed, RAMTotal         float64 // GB
	DiskFree, DiskTotal       float64 // GB
	DiskAct                   float64 // %
	NetDown, NetUp            float64 // bytes/s
	Uptime                    time.Duration
	HistCPU, HistGPU, HistRAM []float64
	HistNet                   []float64
	OK                        bool
}

var (
	pcMu  sync.Mutex
	pcCur PCStats
)

func getPC() PCStats {
	pcMu.Lock()
	defer pcMu.Unlock()
	s := pcCur
	s.HistCPU = append([]float64(nil), pcCur.HistCPU...)
	s.HistGPU = append([]float64(nil), pcCur.HistGPU...)
	s.HistRAM = append([]float64(nil), pcCur.HistRAM...)
	s.HistNet = append([]float64(nil), pcCur.HistNet...)
	return s
}

// ---- PDH

type pdhQuery struct {
	q        uintptr
	counters map[string]uintptr
}

func newPdhQuery() *pdhQuery {
	if pdh.Load() != nil {
		return nil
	}
	q := &pdhQuery{counters: map[string]uintptr{}}
	if r, _, _ := pPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&q.q))); r != 0 {
		return nil
	}
	return q
}

func (q *pdhQuery) add(name, path string) {
	if q == nil {
		return
	}
	var c uintptr
	if r, _, _ := pPdhAddEnglishCounterW.Call(q.q, wstrPtr(path), 0, uintptr(unsafe.Pointer(&c))); r == 0 {
		q.counters[name] = c
	}
}

func (q *pdhQuery) collect() {
	if q != nil {
		pPdhCollectQueryData.Call(q.q)
	}
}

// value devuelve el valor de un contador simple (ok=false si no existe).
func (q *pdhQuery) value(name string) (float64, bool) {
	if q == nil {
		return 0, false
	}
	c, ok := q.counters[name]
	if !ok {
		return 0, false
	}
	var v struct {
		Status uint32
		_      uint32
		Val    float64
	}
	r, _, _ := pPdhGetFormattedCounterValue.Call(c, 0x200|0x8000, 0, uintptr(unsafe.Pointer(&v))) // DOUBLE|NOCAP100
	if r != 0 || (v.Status != 0 && v.Status != 1) {
		return 0, false
	}
	return v.Val, true
}

// array devuelve instancia -> valor de un contador con comodín.
func (q *pdhQuery) array(name string) map[string]float64 {
	out := map[string]float64{}
	if q == nil {
		return out
	}
	c, ok := q.counters[name]
	if !ok {
		return out
	}
	var size, count uint32
	pPdhGetFormattedCounterArrayW.Call(c, 0x200|0x8000, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if size == 0 {
		return out
	}
	buf := make([]byte, size)
	r, _, _ := pPdhGetFormattedCounterArrayW.Call(c, 0x200|0x8000, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return out
	}
	type item struct {
		Name   *uint16
		Status uint32
		_      uint32
		Val    float64
	}
	items := unsafe.Slice((*item)(unsafe.Pointer(&buf[0])), count)
	for _, it := range items {
		if it.Status != 0 && it.Status != 1 {
			continue
		}
		n := utf16PtrToString(uintptr(unsafe.Pointer(it.Name)))
		out[n] += it.Val
	}
	return out
}

// ---- NVIDIA (NVML), si está disponible

type nvml struct {
	dev                                         uintptr
	temp, util, mem, fan, power, clock, getName *syscall.LazyProc
}

func loadNVML() *nvml {
	dll := syscall.NewLazyDLL("nvml.dll")
	if dll.Load() != nil {
		dll = syscall.NewLazyDLL(`C:\Program Files\NVIDIA Corporation\NVSMI\nvml.dll`)
		if dll.Load() != nil {
			return nil
		}
	}
	if r, _, _ := dll.NewProc("nvmlInit_v2").Call(); r != 0 {
		return nil
	}
	n := &nvml{
		temp:    dll.NewProc("nvmlDeviceGetTemperature"),
		util:    dll.NewProc("nvmlDeviceGetUtilizationRates"),
		mem:     dll.NewProc("nvmlDeviceGetMemoryInfo"),
		fan:     dll.NewProc("nvmlDeviceGetFanSpeed"),
		power:   dll.NewProc("nvmlDeviceGetPowerUsage"),
		clock:   dll.NewProc("nvmlDeviceGetClockInfo"),
		getName: dll.NewProc("nvmlDeviceGetName"),
	}
	if r, _, _ := dll.NewProc("nvmlDeviceGetHandleByIndex_v2").Call(0, uintptr(unsafe.Pointer(&n.dev))); r != 0 {
		return nil
	}
	return n
}

func (n *nvml) name() string {
	buf := make([]byte, 96)
	if r, _, _ := n.getName.Call(n.dev, uintptr(unsafe.Pointer(&buf[0])), 96); r != 0 {
		return ""
	}
	if i := strings.IndexByte(string(buf), 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf)
}

func (n *nvml) read(s *PCStats) {
	var t, f, p, clk uint32
	if r, _, _ := n.temp.Call(n.dev, 0, uintptr(unsafe.Pointer(&t))); r == 0 {
		s.GPUTemp = float64(t)
	}
	var u struct{ GPU, Mem uint32 }
	if r, _, _ := n.util.Call(n.dev, uintptr(unsafe.Pointer(&u))); r == 0 {
		s.GPU = float64(u.GPU)
	}
	var m struct{ Total, Free, Used uint64 }
	if r, _, _ := n.mem.Call(n.dev, uintptr(unsafe.Pointer(&m))); r == 0 {
		s.VRAMUsed, s.VRAMTotal = float64(m.Used)/(1<<30), float64(m.Total)/(1<<30)
	}
	if r, _, _ := n.fan.Call(n.dev, uintptr(unsafe.Pointer(&f))); r == 0 {
		s.GPUFan = float64(f)
	}
	if r, _, _ := n.power.Call(n.dev, uintptr(unsafe.Pointer(&p))); r == 0 {
		s.GPUPower = float64(p) / 1000
	}
	if r, _, _ := n.clock.Call(n.dev, 0, uintptr(unsafe.Pointer(&clk))); r == 0 {
		s.GPUMHz = float64(clk)
	}
}

// ---- registro (HKLM)

func regString(path, name string) string {
	var size uint32 = 1024
	buf := make([]uint16, 512)
	r, _, _ := pRegGetValueW.Call(0x80000002, wstrPtr(path), wstrPtr(name), 0x2, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))) // RRF_RT_REG_SZ
	if r != 0 {
		return ""
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf))
}

func regNumber(path, name string) uint64 {
	var v uint64
	var size uint32 = 8
	r, _, _ := pRegGetValueW.Call(0x80000002, wstrPtr(path), wstrPtr(name), 0x18|0x10000, 0, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size))) // DWORD|QWORD, sin expandir
	if r != 0 {
		return 0
	}
	return v
}

func gpuFromRegistry() (string, float64) {
	best, bestMem := "", uint64(0)
	for i := 0; i < 10; i++ {
		k := fmt.Sprintf(`SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}\%04d`, i)
		name := regString(k, "DriverDesc")
		if name == "" {
			continue
		}
		mem := regNumber(k, "HardwareInformation.qwMemorySize")
		if mem == 0 {
			mem = regNumber(k, "HardwareInformation.MemorySize")
		}
		if best == "" || mem > bestMem {
			best, bestMem = name, mem
		}
	}
	return best, float64(bestMem) / (1 << 30)
}

func shortCPUName(n string) string {
	for _, junk := range []string{"(R)", "(TM)", "(tm)", "CPU ", "Processor", "with Radeon Graphics"} {
		n = strings.ReplaceAll(n, junk, "")
	}
	if i := strings.Index(n, " @"); i > 0 {
		n = n[:i]
	}
	if i := strings.Index(n, "-Core"); i > 0 {
		if j := strings.LastIndex(n[:i], " "); j > 0 {
			n = n[:j]
		}
	}
	return strings.Join(strings.Fields(n), " ")
}

func shortGPUName(n string) string {
	for _, junk := range []string{"NVIDIA ", "GeForce ", "AMD ", "Radeon(TM) ", "(R)", "(TM)", "Graphics"} {
		n = strings.ReplaceAll(n, junk, "")
	}
	return strings.Join(strings.Fields(n), " ")
}

func pushHist(h []float64, v float64) []float64 {
	h = append(h, v)
	if len(h) > histLen {
		h = h[len(h)-histLen:]
	}
	return h
}

func pcMonitorWorker() {
	runtime.LockOSThread()
	defer func() { recover() }()
	if demo {
		demoPC()
		return
	}
	q := newPdhQuery()
	q.add("cpu", `\Processor Information(_Total)\% Processor Utility`)
	q.add("cpu2", `\Processor(_Total)\% Processor Time`)
	q.add("perf", `\Processor Information(_Total)\% Processor Performance`)
	q.add("gpu", `\GPU Engine(*)\Utilization Percentage`)
	q.add("vram", `\GPU Adapter Memory(*)\Dedicated Usage`)
	q.add("disk", `\PhysicalDisk(_Total)\% Idle Time`)
	q.add("down", `\Network Interface(*)\Bytes Received/sec`)
	q.add("up", `\Network Interface(*)\Bytes Sent/sec`)
	q.add("temp", `\Thermal Zone Information(*)\Temperature`)
	nv := loadNVML()

	cpuKey := `HARDWARE\DESCRIPTION\System\CentralProcessor\0`
	cpuName := shortCPUName(regString(cpuKey, "ProcessorNameString"))
	baseMHz := float64(regNumber(cpuKey, "~MHz"))
	gpuName, vramTotal := gpuFromRegistry()
	if nv != nil {
		if n := nv.name(); n != "" {
			gpuName = n
		}
	}
	gpuName = shortGPUName(gpuName)

	var st PCStats
	q.collect()
	time.Sleep(time.Second)
	for {
		q.collect()
		s := PCStats{OK: true, CPUName: cpuName, GPUName: gpuName, CPUTemp: -1, GPU: -1, GPUTemp: -1, GPUPower: -1, GPUFan: -1, GPUMHz: -1, VRAMTotal: vramTotal}
		if v, ok := q.value("cpu"); ok {
			s.CPU = math.Min(v, 100)
		} else if v, ok := q.value("cpu2"); ok {
			s.CPU = v
		}
		if v, ok := q.value("perf"); ok && baseMHz > 0 {
			s.CPUMHz = baseMHz * v / 100
		}
		// GPU: como el Administrador de tareas, el máximo entre tipos de motor.
		byType := map[string]float64{}
		for inst, v := range q.array("gpu") {
			if i := strings.Index(inst, "engtype_"); i >= 0 {
				byType[inst[i+8:]] += v
			}
		}
		if len(byType) > 0 {
			m := 0.0
			for _, v := range byType {
				m = math.Max(m, v)
			}
			s.GPU = math.Min(m, 100)
		}
		vr := 0.0
		for _, v := range q.array("vram") {
			vr += v
		}
		s.VRAMUsed = vr / (1 << 30)
		if v, ok := q.value("disk"); ok {
			s.DiskAct = math.Max(0, math.Min(100, 100-v))
		}
		for _, v := range q.array("down") {
			s.NetDown += v
		}
		for _, v := range q.array("up") {
			s.NetUp += v
		}
		for _, v := range q.array("temp") {
			c := v - 273.15
			if c > 15 && c < 115 {
				s.CPUTemp = math.Max(s.CPUTemp, c)
			}
		}
		if nv != nil {
			nv.read(&s)
		}
		var ms struct {
			Length, MemoryLoad                     uint32
			TotalPhys, AvailPhys, TotalPF, AvailPF uint64
			TotalVirt, AvailVirt, AvailExt         uint64
		}
		ms.Length = uint32(unsafe.Sizeof(ms))
		pGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
		s.RAMTotal = float64(ms.TotalPhys) / (1 << 30)
		s.RAMUsed = float64(ms.TotalPhys-ms.AvailPhys) / (1 << 30)
		var free, total, tfree uint64
		pGetDiskFreeSpaceExW.Call(wstrPtr(`C:\`), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&tfree)))
		s.DiskFree, s.DiskTotal = float64(free)/(1<<30), float64(total)/(1<<30)
		up, _, _ := pGetTickCount64.Call()
		s.Uptime = time.Duration(up) * time.Millisecond

		st.HistCPU = pushHist(st.HistCPU, s.CPU)
		st.HistGPU = pushHist(st.HistGPU, math.Max(s.GPU, 0))
		ramPct := 0.0
		if s.RAMTotal > 0 {
			ramPct = s.RAMUsed / s.RAMTotal * 100
		}
		st.HistRAM = pushHist(st.HistRAM, ramPct)
		st.HistNet = pushHist(st.HistNet, s.NetDown)
		s.HistCPU, s.HistGPU, s.HistRAM, s.HistNet = st.HistCPU, st.HistGPU, st.HistRAM, st.HistNet
		pcMu.Lock()
		pcCur = s
		pcMu.Unlock()
		time.Sleep(time.Second)
	}
}

func demoPC() {
	var st PCStats
	t := 0.0
	for {
		t += 1
		s := PCStats{OK: true, CPUName: "Ryzen 7 5800X3D", GPUName: "RTX 4070 SUPER",
			CPU: 30 + 20*math.Sin(t/5), CPUMHz: 4350, CPUTemp: 61,
			GPU: 70 + 25*math.Sin(t/3), GPUTemp: 66, GPUPower: 185, GPUFan: 42, GPUMHz: 2610,
			VRAMUsed: 7.4, VRAMTotal: 12, RAMUsed: 18.6, RAMTotal: 32, DiskFree: 312, DiskTotal: 931,
			DiskAct: 8, NetDown: 2.4e6 * (1 + math.Sin(t/4)), NetUp: 180e3, Uptime: 3*time.Hour + 27*time.Minute}
		st.HistCPU = pushHist(st.HistCPU, s.CPU)
		st.HistGPU = pushHist(st.HistGPU, s.GPU)
		st.HistRAM = pushHist(st.HistRAM, s.RAMUsed/s.RAMTotal*100)
		st.HistNet = pushHist(st.HistNet, s.NetDown)
		s.HistCPU, s.HistGPU, s.HistRAM, s.HistNet = st.HistCPU, st.HistGPU, st.HistRAM, st.HistNet
		pcMu.Lock()
		pcCur = s
		pcMu.Unlock()
		time.Sleep(time.Second)
	}
}

// ---------------------------------------------------------------- dibujo

func fmtBytesRate(b float64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB/s", b/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1f MB/s", b/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.0f KB/s", b/1e3)
	}
	return fmt.Sprintf("%.0f B/s", b)
}

func fmtGB(g float64) string {
	if g >= 1000 {
		return fmt.Sprintf("%.1f TB", g/1024)
	}
	if g >= 100 {
		return fmt.Sprintf("%.0f GB", g)
	}
	return fmt.Sprintf("%.1f GB", g)
}

func monRect(i int) RectF {
	cols, rows := 3, 2
	bottom := pad*1.2 + 10
	cw := (panelW - pad*float32(cols+1)) / float32(cols)
	ch := (panelH - pad*float32(rows) - bottom) / float32(rows)
	return RectF{pad + float32(i%cols)*(cw+pad), pad + float32(i/cols)*(ch+pad), cw, ch}
}

func sparkline(c *Canvas, r RectF, h []float64, maxV float64, col uint32) {
	if len(h) < 2 {
		return
	}
	if maxV <= 0 {
		for _, v := range h {
			maxV = math.Max(maxV, v)
		}
		if maxV <= 0 {
			maxV = 1
		}
		maxV *= 1.15
	}
	// la línea siempre ocupa todo el ancho, aunque haya pocos datos todavía
	step := r.W / float32(len(h)-1)
	x0 := r.X + r.W - step*float32(len(h)-1)
	pts := make([]PointF, 0, len(h)+2)
	for i, v := range h {
		y := r.Y + r.H - float32(math.Min(v/maxV, 1))*r.H
		pts = append(pts, PointF{x0 + step*float32(i), y})
	}
	area := append([]PointF{{x0, r.Y + r.H}}, pts...)
	area = append(area, PointF{r.X + r.W, r.Y + r.H})
	c.FillPoly(area, col&0x00FFFFFF|0x30000000)
	c.Polyline(pts, r.H*0.03+1, col)
}

func bar(c *Canvas, x, y, w, h float32, frac float64, col uint32) {
	c.RoundRect(x, y, w, h, h/2, 0x22FFFFFF)
	if frac > 0 {
		c.RoundRect(x, y, w*float32(math.Min(frac, 1)), h, h/2, col)
	}
}

type monCard struct {
	title, big, unit string
	lines            []string
	hist             []float64
	histMax          float64
	barFrac          float64 // < 0 = sin barra
	ringVal          float64 // < 0 = sin anillo (0-100)
	accent           uint32
}

func drawMonCard(c *Canvas, r RectF, m monCard) {
	rad := min32(r.W, r.H) * 0.16
	flatCard(c, r.X, r.Y, r.W, r.H, rad, themeTile(0xFF23252A))
	p := r.H * 0.09
	fs := r.H * 0.085
	// gráfico al fondo
	if m.hist != nil {
		gr := RectF{r.X, r.Y + r.H*0.62, r.W, r.H * 0.38}
		c.ClipRoundRect(r.X, r.Y, r.W, r.H, rad)
		sparkline(c, gr, m.hist, m.histMax, m.accent)
		c.ResetClip()
	}
	c.Circle(r.X+p+fs*0.35, r.Y+p+fs*0.75, fs*0.3, m.accent)
	c.Text(m.title, semiFont, fs, semiStyle, colGrey, RectF{r.X + p + fs, r.Y + p, r.W - 2*p - fs, fs * 1.5}, AlignNear, AlignCenter)
	bigW := r.W - 2*p
	if m.ringVal >= 0 {
		// anillo con el porcentaje adentro
		rr := r.H * 0.17
		cx, cy := r.X+r.W-p-rr*1.1, r.Y+p+rr*1.15
		ring(c, cx, cy, rr, float32(m.ringVal/100), loadColor(m.ringVal))
		c.Text(fmt.Sprintf("%.0f%%", m.ringVal), semiFont, rr*0.55, semiStyle, colWhite,
			RectF{cx - rr, cy - rr, rr * 2, rr * 2}, AlignCenter, AlignCenter)
		bigW = r.W - 3*p - rr*2.2
	}
	bigS := r.H * 0.155
	c.Text(m.big, semiFont, bigS, semiStyle, colWhite, RectF{r.X + p, r.Y + p + fs*1.4, bigW, bigS * 1.35}, AlignNear, AlignCenter)
	y := r.Y + p + fs*1.4 + bigS*1.35
	if m.barFrac >= 0 {
		bar(c, r.X+p, y+fs*0.1, r.W-2*p, fs*0.35, m.barFrac, m.accent)
		y += fs * 0.7
	}
	for _, l := range m.lines {
		c.Text(l, textFont, fs*0.92, FontStyleRegular, 0xFFC8CCD2, RectF{r.X + p, y, r.W - 2*p, fs * 1.45}, AlignNear, AlignCenter)
		y += fs * 1.35
	}
}

func paintMonitorPage(c *Canvas) {
	s := getPC()
	if !s.OK {
		c.Text("Leyendo datos del PC…", textFont, panelH*0.04, FontStyleRegular, colGrey,
			RectF{0, 0, panelW, panelH}, AlignCenter, AlignCenter)
		return
	}
	// CPU
	cpuLines := []string{s.CPUName}
	extra := []string{}
	if s.CPUMHz > 0 {
		extra = append(extra, fmt.Sprintf("%.2f GHz", s.CPUMHz/1000))
	}
	if s.CPUTemp >= 0 {
		extra = append(extra, fmt.Sprintf("%.0f °C", s.CPUTemp))
	}
	if len(extra) > 0 {
		cpuLines = append(cpuLines, strings.Join(extra, " · "))
	}
	cpuBig := "—"
	cpuLines = []string{s.CPUName}
	if s.CPUTemp >= 0 {
		cpuBig = fmt.Sprintf("%.0f °C", s.CPUTemp)
		if s.CPUMHz > 0 {
			cpuLines = append(cpuLines, fmt.Sprintf("%.2f GHz", s.CPUMHz/1000))
		}
	} else if s.CPUMHz > 0 {
		cpuBig = fmt.Sprintf("%.2f GHz", s.CPUMHz/1000)
	}
	drawMonCard(c, monRect(0), monCard{title: "CPU", big: cpuBig, lines: cpuLines,
		hist: s.HistCPU, histMax: 100, barFrac: -1, ringVal: s.CPU, accent: 0xFF4FC3F7})

	// GPU
	gpuBig := "—"
	gl := []string{s.GPUName}
	ge := []string{}
	if s.GPUTemp >= 0 {
		gpuBig = fmt.Sprintf("%.0f °C", s.GPUTemp)
	} else if s.VRAMUsed > 0 {
		gpuBig = fmt.Sprintf("%.1f GB", s.VRAMUsed)
	}
	if s.GPUPower >= 0 {
		ge = append(ge, fmt.Sprintf("%.0f W", s.GPUPower))
	}
	if s.GPUFan >= 0 {
		ge = append(ge, fmt.Sprintf("Vent. %.0f %%", s.GPUFan))
	}
	if len(ge) > 0 {
		gl = append(gl, strings.Join(ge, " · "))
	}
	if s.VRAMTotal > 0 {
		gl = append(gl, fmt.Sprintf("VRAM %.1f / %.0f GB", s.VRAMUsed, s.VRAMTotal))
	} else if s.VRAMUsed > 0 {
		gl = append(gl, fmt.Sprintf("VRAM %.1f GB", s.VRAMUsed))
	}
	drawMonCard(c, monRect(1), monCard{title: "GPU", big: gpuBig, lines: gl, hist: s.HistGPU, histMax: 100, barFrac: -1, ringVal: s.GPU, accent: 0xFF81C784})

	// RAM
	ramFrac := 0.0
	if s.RAMTotal > 0 {
		ramFrac = s.RAMUsed / s.RAMTotal
	}
	drawMonCard(c, monRect(2), monCard{title: "RAM", big: fmtRAM(s.RAMUsed),
		lines: []string{fmt.Sprintf("en uso de %.0f GB", math.Round(s.RAMTotal))},
		hist:  s.HistRAM, histMax: 100, barFrac: -1, ringVal: ramFrac * 100, accent: 0xFFB39DDB})

	// Disco
	diskFrac := 0.0
	if s.DiskTotal > 0 {
		diskFrac = 1 - s.DiskFree/s.DiskTotal
	}
	drawMonCard(c, monRect(3), monCard{title: "Disco C:", big: fmtGB(s.DiskFree),
		lines:   []string{"libres de " + fmtGB(s.DiskTotal), fmt.Sprintf("Actividad %.0f %%", s.DiskAct)},
		barFrac: diskFrac, ringVal: -1, accent: 0xFFFFB74D})

	// Red
	drawMonCard(c, monRect(4), monCard{title: "Red", big: "↓ " + fmtBytesRate(s.NetDown),
		lines: []string{"↑ " + fmtBytesRate(s.NetUp)}, hist: s.HistNet, barFrac: -1, ringVal: -1, accent: 0xFF4DD0E1})

	// Sistema
	h := int(s.Uptime.Hours())
	mnt := int(s.Uptime.Minutes()) % 60
	up := fmt.Sprintf("%d h %d min", h, mnt)
	if h >= 24 {
		up = fmt.Sprintf("%d d %d h", h/24, h%24)
	}
	now := time.Now()
	dias := []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	meses := []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
	drawMonCard(c, monRect(5), monCard{title: "Sistema", big: now.Format("15:04"),
		lines:   []string{fmt.Sprintf("%s %d de %s", dias[now.Weekday()], now.Day(), meses[now.Month()-1]), "Encendido hace " + up},
		barFrac: -1, ringVal: -1, accent: 0xFF90A4AE})
}

func fmtRAM(g float64) string {
	if g >= 10 {
		return fmt.Sprintf("%.0f GB", g)
	}
	return fmt.Sprintf("%.1f GB", g)
}
