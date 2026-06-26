package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"go.bug.st/serial"
)

const (
	frameWidth  = 80
	frameHeight = 60
	frameSize   = frameWidth * frameHeight * 2 // uint16 per pixel
	centerIndex = (frameHeight/2)*frameWidth + frameWidth/2
)

// ThermalFrame is the payload emitted on "device:frame". It mirrors the
// DeviceFrame interface consumed by useDeviceState on the Vue side.
type ThermalFrame struct {
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Temps  []float64 `json:"temps"` // row-major, degrees C
	Min    float64   `json:"min"`
	Max    float64   `json:"max"`
	Center float64   `json:"center"`
	FPS    int       `json:"fps"`
	TS     int64     `json:"ts"`
}

// DeviceStatus is emitted on "device:status" so the UI can reflect the link.
type DeviceStatus struct {
	Connected bool   `json:"connected"`
	Streaming bool   `json:"streaming"`
	SystemID  string `json:"systemId,omitempty"`
}

// Region is a tracked ROI. X/Y are normalized (0..1) within the frame and
// mark its center. W/H are normalized box dimensions: 0/0 means the ROI is
// a single-pixel point sample; non-zero means the recorded value is the
// average temperature over that box (see sampleRegion below — it mirrors
// the frontend's sampleRegionValue exactly, so the live readout and the
// recorded CSV value always agree).
type Region struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
	W  float64 `json:"w"`
	H  float64 `json:"h"`
}

type App struct {
	ctx context.Context

	// mu guards every mutable field below. The serial read goroutine never
	// reads these directly while blocked on I/O — it works off locals passed
	// into readLoop — so a Disconnect on another goroutine can't race it.
	mu              sync.Mutex
	port            serial.Port
	stopCh          chan struct{}
	deviceConnected bool
	connectedPort   string

	streaming   bool
	recording   bool
	captureRate int // milliseconds between recorded samples

	regions []Region

	// recording sinks (per-frame PNG series + ROI chart data)
	recDir       string // unique session folder chosen by the user
	framesDir    string // recDir/frames
	frameSeq     int    // running image counter for this session
	csvFile      *os.File
	csvWriter    *csv.Writer
	recRegions   []Region // column set, frozen at record start
	lastRecWrite time.Time

	portWatchStop chan struct{}
}

func NewApp() *App {
	return &App{
		captureRate: 100,
		// Seed the defaults the Vue composable also starts with, so CSV columns
		// exist even before the user adds ROIs via "Define New" or by
		// clicking/dragging on the live feed.
		regions: []Region{
			{ID: "ROI_01", X: 0.31, Y: 0.42},
			{ID: "ROI_02", X: 0.66, Y: 0.55},
		},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.portWatchStop = make(chan struct{})
	go a.watchPorts()
}

// shutdown is wired via OnShutdown in main.go.
func (a *App) shutdown(_ context.Context) {
	if a.portWatchStop != nil {
		close(a.portWatchStop)
	}
	_ = a.DisconnectSerial()
}

/* -------------------------------------------------------------------------- */
/* Port discovery                                                             */
/* -------------------------------------------------------------------------- */

func (a *App) watchPorts() {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ports, err := serial.GetPortsList(); err == nil {
			runtime.EventsEmit(a.ctx, "ports:list", ports)
		}
		select {
		case <-a.portWatchStop:
			return
		case <-ticker.C:
		}
	}
}

/* -------------------------------------------------------------------------- */
/* Connection lifecycle                                                       */
/* -------------------------------------------------------------------------- */

func (a *App) ConnectSerial(name string) error {
	_ = a.DisconnectSerial() // tear down any previous link first

	p, err := serial.Open(name, &serial.Mode{BaudRate: 921600})
	if err != nil {
		return err
	}

	stopCh := make(chan struct{})

	a.mu.Lock()
	a.port = p
	a.stopCh = stopCh
	a.connectedPort = name
	a.deviceConnected = true
	a.streaming = true // stream immediately, like the reference script
	a.mu.Unlock()

	// readLoop owns its port + stopCh as locals — no shared-field reads in the hot path.
	go a.readLoop(p, stopCh)

	a.emitStatus()
	return nil
}

func (a *App) DisconnectSerial() error {
	a.mu.Lock()
	if !a.deviceConnected {
		a.mu.Unlock()
		return nil
	}
	a.resetConnectionLocked()
	a.mu.Unlock()

	a.emitStatus()
	return nil
}

// resetConnectionLocked must be called with a.mu held.
func (a *App) resetConnectionLocked() {
	if a.stopCh != nil {
		close(a.stopCh)
		a.stopCh = nil
	}
	if a.port != nil {
		a.port.Close()
		a.port = nil
	}
	a.closeRecordingLocked()
	a.connectedPort = ""
	a.deviceConnected = false
	a.streaming = false
}

// handleDeviceLost runs when readLoop breaks on a read error (cable pulled, etc).
// The stopCh-identity guard makes this a no-op if the user already disconnected.
func (a *App) handleDeviceLost(stopCh chan struct{}) {
	a.mu.Lock()
	if a.stopCh != stopCh { // a Disconnect already cleaned this link up
		a.mu.Unlock()
		return
	}
	a.resetConnectionLocked()
	a.mu.Unlock()

	a.emitStatus()
}

func (a *App) GetConnectedPort() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connectedPort
}

/* -------------------------------------------------------------------------- */
/* Stream / recording / region commands (bound to the frontend)              */
/* -------------------------------------------------------------------------- */

func (a *App) StartStream() {
	a.mu.Lock()
	a.streaming = true
	a.mu.Unlock()
	a.emitStatus()
}

func (a *App) StopStream() {
	a.mu.Lock()
	a.streaming = false
	a.mu.Unlock()
	a.emitStatus()
}

// StartRecording asks the user where to drop a recording folder, then creates
// a unique thermal_<unix>/ directory there containing:
//
//	frames/      one PNG per captured frame (frame_000001.png, ...)
//	data.csv     timestamp + image_file + frame stats + per-ROI columns
//
// It returns the absolute path of the created folder so the UI can surface
// where the data landed. If the user cancels the directory dialog, it returns
// ("", nil) and recording does NOT start.
func (a *App) StartRecording() (string, error) {
	// Open the picker BEFORE taking the lock — a dialog can stay open for a
	// long time and must never block other mutex users (Disconnect, toggles).
	parent, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose where to save this recording",
	})
	if err != nil {
		return "", err
	}
	if parent == "" {
		return "", nil // user cancelled
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.recording {
		return "", nil
	}

	base := fmt.Sprintf("thermal_%d", time.Now().Unix())
	dir := filepath.Join(parent, base)
	framesDir := filepath.Join(dir, "frames")
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return "", err
	}

	csvPath := filepath.Join(dir, "data.csv")
	csvFile, err := os.Create(csvPath)
	if err != nil {
		return "", err
	}

	a.recDir = dir
	a.framesDir = framesDir
	a.frameSeq = 0
	a.csvFile = csvFile
	a.csvWriter = csv.NewWriter(csvFile)
	a.recRegions = append([]Region(nil), a.regions...) // freeze columns
	a.lastRecWrite = time.Time{}
	a.recording = true

	// Header: time + image filename + frame stats + one column per ROI. Area
	// ROIs (W/H > 0) get an "_avg" suffix so a CSV reader can tell a box
	// average from a single-pixel point sample at a glance.
	header := []string{"timestamp_ms", "image_file", "min_c", "max_c", "center_c"}
	for _, r := range a.recRegions {
		col := r.ID
		if r.W > 0 || r.H > 0 {
			col += "_avg"
		}
		header = append(header, col)
	}
	_ = a.csvWriter.Write(header)
	a.csvWriter.Flush()

	return dir, nil
}

func (a *App) StopRecording() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.recording {
		return nil
	}
	a.closeRecordingLocked()
	return nil
}

// closeRecordingLocked flushes and closes the CSV sink and clears the frame
// series state. Must hold a.mu.
func (a *App) closeRecordingLocked() {
	if a.csvWriter != nil {
		a.csvWriter.Flush()
		a.csvWriter = nil
	}
	if a.csvFile != nil {
		a.csvFile.Close()
		a.csvFile = nil
	}
	a.recRegions = nil
	a.recDir = ""
	a.framesDir = ""
	a.frameSeq = 0
	a.recording = false
}

func (a *App) SetCaptureRate(ms int) error {
	if ms < 1 {
		ms = 1
	}
	a.mu.Lock()
	a.captureRate = ms
	a.mu.Unlock()
	return nil
}

// DefineRegion adds a new ROI. w/h are normalized box dimensions; pass 0/0
// for a single-pixel point ROI, or non-zero to make this an area ROI whose
// recorded value is the box average (see sampleRegion).
func (a *App) DefineRegion(id string, x, y, w, h float64) error {
	a.mu.Lock()
	a.regions = append(a.regions, Region{ID: id, X: x, Y: y, W: w, H: h})
	a.mu.Unlock()
	return nil
}

func (a *App) ClearRegions() error {
	a.mu.Lock()
	a.regions = nil
	a.mu.Unlock()
	return nil
}

/* -------------------------------------------------------------------------- */
/* Frame ingestion                                                            */
/* -------------------------------------------------------------------------- */

func waitForHeader(r *bufio.Reader) error {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if b != 0xAA {
			continue
		}
		b, err = r.ReadByte()
		if err != nil {
			return err
		}
		if b == 0x55 {
			return nil
		}
	}
}

func (a *App) readLoop(port serial.Port, stopCh chan struct{}) {
	reader := bufio.NewReader(port)
	raw := make([]byte, frameSize)

	var fps float64
	var lastFrame time.Time

	for {
		select {
		case <-stopCh:
			return
		default:
		}

		if err := waitForHeader(reader); err != nil {
			break
		}
		if _, err := io.ReadFull(reader, raw); err != nil {
			break
		}

		// Decode uint16 LE -> Celsius (raw/100 - 273.15), 1-decimal resolution.
		temps := make([]float64, frameWidth*frameHeight)
		minT, maxT := math.MaxFloat64, -math.MaxFloat64
		for i := range temps {
			v := binary.LittleEndian.Uint16(raw[i*2:])
			t := math.Round((float64(v)/100.0-273.15)*10) / 10
			temps[i] = t
			if t < minT {
				minT = t
			}
			if t > maxT {
				maxT = t
			}
		}

		now := time.Now()
		if !lastFrame.IsZero() {
			if dt := now.Sub(lastFrame).Seconds(); dt > 0 {
				inst := 1.0 / dt
				if fps == 0 {
					fps = inst
				} else {
					fps = fps*0.9 + inst*0.1 // smooth
				}
			}
		}
		lastFrame = now

		// Snapshot config + write a recorded sample under the lock. The blocking
		// serial reads above happen WITHOUT the lock, so this can't stall I/O.
		// PNG encoding of an 80x60 frame is sub-millisecond, so doing it here
		// is consistent with the existing CSV-under-lock design.
		a.mu.Lock()
		streaming := a.streaming
		if a.recording &&
			now.Sub(a.lastRecWrite) >= time.Duration(a.captureRate)*time.Second {
			// Write this frame as a PNG and reference it from the CSV row, so
			// each image lines up with its timestamp + ROI samples.
			imgRel := ""
			if a.framesDir != "" {
				a.frameSeq++
				name := fmt.Sprintf("frame_%06d.png", a.frameSeq)
				if err := writeFramePNG(filepath.Join(a.framesDir, name), temps, minT, maxT); err != nil {
					runtime.LogErrorf(a.ctx, "frame png write failed: %v", err)
				} else {
					imgRel = filepath.ToSlash(filepath.Join("frames", name))
				}
			}
			if a.csvWriter != nil {
				row := make([]string, 0, 5+len(a.recRegions))
				row = append(row,
					strconv.FormatInt(now.UnixMilli(), 10),
					imgRel,
					f2(minT), f2(maxT), f2(temps[centerIndex]),
				)
				for _, r := range a.recRegions {
					row = append(row, f2(sampleRegion(temps, r)))
				}
				_ = a.csvWriter.Write(row)
				a.csvWriter.Flush()
			}
			a.lastRecWrite = now
		}
		a.mu.Unlock()

		if streaming && a.ctx != nil {
			runtime.EventsEmit(a.ctx, "device:frame", ThermalFrame{
				Width:  frameWidth,
				Height: frameHeight,
				Temps:  temps,
				Min:    minT,
				Max:    maxT,
				Center: temps[centerIndex],
				FPS:    int(math.Round(fps)),
				TS:     now.UnixMilli(),
			})
		}
	}

	a.handleDeviceLost(stopCh)
}

/* -------------------------------------------------------------------------- */
/* Frame image encoding                                                       */
/* -------------------------------------------------------------------------- */

// infernoStops approximates the perceptually-uniform "inferno" colormap.
var infernoStops = [...][3]float64{
	{0, 0, 4},
	{40, 11, 84},
	{101, 21, 110},
	{159, 42, 99},
	{212, 72, 66},
	{245, 125, 21},
	{250, 193, 39},
	{252, 255, 164},
}

// heat maps a normalized value (0..1) to an inferno-ish RGBA color.
func heat(norm float64) color.RGBA {
	if norm < 0 {
		norm = 0
	}
	if norm > 1 {
		norm = 1
	}
	seg := norm * float64(len(infernoStops)-1)
	i := int(seg)
	if i >= len(infernoStops)-1 {
		c := infernoStops[len(infernoStops)-1]
		return color.RGBA{R: uint8(c[0]), G: uint8(c[1]), B: uint8(c[2]), A: 255}
	}
	f := seg - float64(i)
	lo := infernoStops[i]
	hi := infernoStops[i+1]
	return color.RGBA{
		R: uint8(lo[0] + (hi[0]-lo[0])*f),
		G: uint8(lo[1] + (hi[1]-lo[1])*f),
		B: uint8(lo[2] + (hi[2]-lo[2])*f),
		A: 255,
	}
}

// writeFramePNG renders an 80x60 thermal frame to a PNG, colorized with the
// inferno map normalized to this frame's own min/max. Absolute temperatures
// are preserved in the CSV, so the image is purely for visual review.
func writeFramePNG(path string, temps []float64, minT, maxT float64) error {
	img := image.NewRGBA(image.Rect(0, 0, frameWidth, frameHeight))
	span := maxT - minT
	for y := 0; y < frameHeight; y++ {
		for x := 0; x < frameWidth; x++ {
			t := temps[y*frameWidth+x]
			norm := 0.0
			if span > 0 {
				norm = (t - minT) / span
			}
			img.Set(x, y, heat(norm))
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

/* -------------------------------------------------------------------------- */
/* Helpers                                                                    */
/* -------------------------------------------------------------------------- */

// sampleRegion mirrors the frontend's sampleRegionValue: it reads a single
// pixel for a point ROI (W==H==0), or averages every pixel inside the
// normalized box for an area ROI.
func sampleRegion(temps []float64, r Region) float64 {
	if r.W <= 0 && r.H <= 0 {
		px := clampInt(int(math.Round(r.X*float64(frameWidth-1))), 0, frameWidth-1)
		py := clampInt(int(math.Round(r.Y*float64(frameHeight-1))), 0, frameHeight-1)
		return temps[py*frameWidth+px]
	}

	x0 := clampInt(int(math.Round((r.X-r.W/2)*float64(frameWidth-1))), 0, frameWidth-1)
	x1 := clampInt(int(math.Round((r.X+r.W/2)*float64(frameWidth-1))), 0, frameWidth-1)
	y0 := clampInt(int(math.Round((r.Y-r.H/2)*float64(frameHeight-1))), 0, frameHeight-1)
	y1 := clampInt(int(math.Round((r.Y+r.H/2)*float64(frameHeight-1))), 0, frameHeight-1)
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}

	sum := 0.0
	n := 0
	for yy := y0; yy <= y1; yy++ {
		row := yy * frameWidth
		for xx := x0; xx <= x1; xx++ {
			sum += temps[row+xx]
			n++
		}
	}
	if n == 0 {
		return temps[y0*frameWidth+x0]
	}
	return sum / float64(n)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func f2(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func (a *App) emitStatus() {
	a.mu.Lock()
	st := DeviceStatus{
		Connected: a.deviceConnected,
		Streaming: a.streaming,
		SystemID:  a.connectedPort,
	}
	a.mu.Unlock()

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "device:status", st)
	}
}
