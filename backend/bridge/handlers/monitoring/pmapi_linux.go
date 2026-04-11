//go:build linux

package monitoring

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ─── PMAPI constants ─────────────────────────────────────────────────────────

const (
	pmContextHost    = 1
	pmContextArchive = 2

	pmModeInterp = 1
	pmModeForw   = 2

	pmTypeDouble = 5

	pmIndomNull = 0xFFFFFFFF

	pmErrEOL = -12370 // PM_ERR_EOL: End of PCP archive
)

// ─── C struct offsets (x86_64 Linux, libpcp.so.3 / PCP_3.0 ABI) ────────────
//
//	pmResult { struct timeval(16), int numpmid(4), pad(4), *pmValueSet[] }
//	pmValueSet { pmID(4), int numval(4), int valfmt(4), pad(4), pmValue[] }
//	pmValue { int inst(4), pad(4), union value(8) }  → 16 bytes each
//	pmDesc { pmID(4), int type(4), pmInDom(4), int sem(4), pmUnits(4) }

const (
	resultTimestampOff = 0  // struct timeval at start
	resultNumpmidOff   = 16 // after 16-byte timeval
	resultVsetOff      = 24 // after numpmid(4) + pad(4)

	vsetPMIDOff    = 0
	vsetNumvalOff  = 4
	vsetValfmtOff  = 8
	vsetVlistOff   = 16 // after pmid(4) + numval(4) + valfmt(4) + pad(4)
	sizeofPMValue  = 16 // inst(4) + pad(4) + union(8)
	pmValueInstOff = 0
	pmValueValOff  = 8 // offset of the value union within pmValue

	descTypeOff  = 4
	descIndomOff = 8
)

// ─── libpcp function pointers ────────────────────────────────────────────────

var (
	pmapiOnce sync.Once
	pmapiErr  error
	pmapiMu   sync.Mutex

	_pmNewContext      func(ctxType int32, name string) int32
	_pmDestroyContext  func(handle int32) int32
	_pmLookupDesc      func(pmid uint32, desc unsafe.Pointer) int32
	_pmSetMode         func(mode int32, when unsafe.Pointer, delta int32) int32
	_pmFetch           func(numpmid int32, pmidlist unsafe.Pointer, result unsafe.Pointer) int32
	_pmFreeResult      func(result unsafe.Pointer)
	_pmExtractValue    func(valfmt int32, ival unsafe.Pointer, itype int32, oval unsafe.Pointer, otype int32) int32
	_pmGetArchiveEnd   func(tvp unsafe.Pointer) int32
	_pmGetArchiveLabel func(lp unsafe.Pointer) int32

	// resolved via SyscallN because char** / int** output params
	_pmLookupNameAddr      uintptr
	_pmGetInDomAddr        uintptr
	_pmGetInDomArchiveAddr uintptr
	_cFreeAddr             uintptr
)

// withPMAPI serializes all libpcp calls and pins the calling goroutine to one
// OS thread for the lifetime of the PMAPI context. PMAPI keeps the current
// context in thread-local state, and concurrent purego calls into libpcp have
// proven unstable on real hosts.
func withPMAPI[T any](fn func() (T, error)) (T, error) {
	var zero T
	if err := ensurePMAPI(); err != nil {
		return zero, err
	}

	pmapiMu.Lock()
	defer pmapiMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	return fn()
}

func ensurePMAPI() error {
	pmapiOnce.Do(func() {
		pmapiErr = loadLibPCP()
	})
	return pmapiErr
}

func loadLibPCP() error {
	var lib uintptr
	var err error

	for _, path := range []string{"libpcp.so.3", "libpcp.so"} {
		lib, err = purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("cannot load libpcp: %w", err)
	}

	purego.RegisterLibFunc(&_pmNewContext, lib, "pmNewContext")
	purego.RegisterLibFunc(&_pmDestroyContext, lib, "pmDestroyContext")
	purego.RegisterLibFunc(&_pmLookupDesc, lib, "pmLookupDesc")
	purego.RegisterLibFunc(&_pmSetMode, lib, "pmSetMode")
	purego.RegisterLibFunc(&_pmFetch, lib, "pmFetch")
	purego.RegisterLibFunc(&_pmFreeResult, lib, "pmFreeResult")
	purego.RegisterLibFunc(&_pmExtractValue, lib, "pmExtractValue")
	purego.RegisterLibFunc(&_pmGetArchiveEnd, lib, "pmGetArchiveEnd")
	purego.RegisterLibFunc(&_pmGetArchiveLabel, lib, "pmGetArchiveLabel")

	var lookupErr error
	_pmLookupNameAddr, lookupErr = purego.Dlsym(lib, "pmLookupName")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve pmLookupName: %w", lookupErr)
	}
	_pmGetInDomAddr, lookupErr = purego.Dlsym(lib, "pmGetInDom")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve pmGetInDom: %w", lookupErr)
	}
	_pmGetInDomArchiveAddr, lookupErr = purego.Dlsym(lib, "pmGetInDomArchive")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve pmGetInDomArchive: %w", lookupErr)
	}
	_cFreeAddr, lookupErr = purego.Dlsym(lib, "free")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve free: %w", lookupErr)
	}

	return nil
}

// ─── low-level helpers ───────────────────────────────────────────────────────

// cString returns a null-terminated byte slice suitable for passing to C.
func cString(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

// goStringFromPtr reads a null-terminated C string into a Go string.
func goStringFromPtr(ptr unsafe.Pointer) string {
	if ptr == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
		if n > 4096 {
			break
		}
	}
	return string(unsafe.Slice((*byte)(ptr), n))
}

func pmapiErrString(rc int32) string {
	return fmt.Sprintf("PMAPI error %d", rc)
}

// ─── PMAPI wrappers ──────────────────────────────────────────────────────────

func pmapiNewContext(ctxType int32, name string) (int32, error) {
	if err := ensurePMAPI(); err != nil {
		return -1, err
	}
	handle := _pmNewContext(ctxType, name)
	if handle < 0 {
		return -1, fmt.Errorf("pmNewContext(%d, %q): %s", ctxType, name, pmapiErrString(handle))
	}
	return handle, nil
}

func pmapiLookupPMID(metric string) (uint32, error) {
	nameBytes := cString(metric)
	namePtr := unsafe.Pointer(&nameBytes[0])
	namePtrPtr := unsafe.Pointer(&namePtr)
	var pmid uint32
	r1, _, _ := purego.SyscallN(_pmLookupNameAddr, 1, uintptr(namePtrPtr), uintptr(unsafe.Pointer(&pmid)))
	runtime.KeepAlive(nameBytes)
	rc := int32(r1)
	if rc < 0 {
		return 0, fmt.Errorf("pmLookupName(%q): %s", metric, pmapiErrString(rc))
	}
	return pmid, nil
}

type pmapiDesc struct {
	metricType int32
	indom      uint32
}

func pmapiLookupDesc(pmid uint32) (pmapiDesc, error) {
	// pmDesc is 20 bytes: pmID(4) + type(4) + indom(4) + sem(4) + units(4)
	var buf [20]byte
	rc := _pmLookupDesc(pmid, unsafe.Pointer(&buf[0]))
	if rc < 0 {
		return pmapiDesc{}, fmt.Errorf("pmLookupDesc: %s", pmapiErrString(rc))
	}
	return pmapiDesc{
		metricType: *(*int32)(unsafe.Pointer(&buf[descTypeOff])),
		indom:      *(*uint32)(unsafe.Pointer(&buf[descIndomOff])),
	}, nil
}

func pmapiGetInDomMap(indom uint32, archive bool) (map[string]int32, error) {
	sym := _pmGetInDomAddr
	label := "pmGetInDom"
	if archive {
		sym = _pmGetInDomArchiveAddr
		label = "pmGetInDomArchive"
	}
	var instlistPtr unsafe.Pointer
	var namelistPtr unsafe.Pointer
	r1, _, _ := purego.SyscallN(sym,
		uintptr(indom),
		uintptr(unsafe.Pointer(&instlistPtr)),
		uintptr(unsafe.Pointer(&namelistPtr)),
	)
	rc := int32(r1)
	if rc < 0 {
		return nil, fmt.Errorf("%s: %s", label, pmapiErrString(rc))
	}
	n := int(rc)

	result := make(map[string]int32, n)
	for i := range n {
		instID := *(*int32)(unsafe.Add(instlistPtr, uintptr(i)*4))
		namePtr := *(*unsafe.Pointer)(unsafe.Add(namelistPtr, uintptr(i)*unsafe.Sizeof(uintptr(0))))
		name := goStringFromPtr(namePtr)
		result[name] = instID
	}

	// Free the C-allocated arrays.
	if instlistPtr != nil {
		purego.SyscallN(_cFreeAddr, uintptr(instlistPtr))
	}
	if namelistPtr != nil {
		purego.SyscallN(_cFreeAddr, uintptr(namelistPtr))
	}

	return result, nil
}

func pmapiGetArchiveEnd(handle int32) (time.Time, error) {
	// struct timeval { int64 sec, int64 usec } = 16 bytes
	var tv [16]byte
	rc := _pmGetArchiveEnd(unsafe.Pointer(&tv[0]))
	if rc < 0 {
		return time.Time{}, fmt.Errorf("pmGetArchiveEnd: %s", pmapiErrString(rc))
	}
	sec := *(*int64)(unsafe.Pointer(&tv[0]))
	usec := *(*int64)(unsafe.Pointer(&tv[8]))
	return time.Unix(sec, usec*1000), nil
}

func pmapiGetArchiveStart(handle int32) (time.Time, error) {
	// pmLogLabel: { int magic(4), pid_t pid(4), struct timeval start(16), ... }
	// The start timeval is at offset 8.
	var label [128]byte
	rc := _pmGetArchiveLabel(unsafe.Pointer(&label[0]))
	if rc < 0 {
		return time.Time{}, fmt.Errorf("pmGetArchiveLabel: %s", pmapiErrString(rc))
	}
	sec := *(*int64)(unsafe.Pointer(&label[8]))
	usec := *(*int64)(unsafe.Pointer(&label[16]))
	return time.Unix(sec, usec*1000), nil
}

func pmapiSetMode(mode int32, when time.Time, deltaMs int32) error {
	sec := when.Unix()
	usec := int64(when.Nanosecond() / 1000)
	var tv [16]byte
	*(*int64)(unsafe.Pointer(&tv[0])) = sec
	*(*int64)(unsafe.Pointer(&tv[8])) = usec
	rc := _pmSetMode(mode, unsafe.Pointer(&tv[0]), deltaMs)
	if rc < 0 {
		return fmt.Errorf("pmSetMode: %s", pmapiErrString(rc))
	}
	return nil
}

// pmapiResultReader holds a fetched pmResult and extracts values from it.
type pmapiResultReader struct {
	ptr unsafe.Pointer
}

func pmapiFetch(pmid uint32) (*pmapiResultReader, error) {
	var resultPtr unsafe.Pointer
	rc := _pmFetch(1, unsafe.Pointer(&pmid), unsafe.Pointer(&resultPtr))
	if rc < 0 {
		if rc == int32(pmErrEOL) {
			return nil, errPMAPIEOL
		}
		return nil, fmt.Errorf("pmFetch: %s", pmapiErrString(rc))
	}
	return &pmapiResultReader{ptr: resultPtr}, nil
}

func (r *pmapiResultReader) free() {
	if r.ptr != nil {
		_pmFreeResult(r.ptr)
		r.ptr = nil
	}
}

func (r *pmapiResultReader) timestamp() time.Time {
	sec := *(*int64)(unsafe.Add(r.ptr, resultTimestampOff))
	usec := *(*int64)(unsafe.Add(r.ptr, resultTimestampOff+8))
	return time.Unix(sec, usec*1000)
}

// extractSum sums the double values across selected instances in the first value set.
// If wantInsts is nil, all instances are summed.
func (r *pmapiResultReader) extractSum(desc pmapiDesc, wantInsts map[int32]bool) (float64, bool) {
	numpmid := *(*int32)(unsafe.Add(r.ptr, resultNumpmidOff))
	if numpmid < 1 {
		return 0, false
	}

	// Read first vset pointer from the flexible array.
	vsetPtr := *(*unsafe.Pointer)(unsafe.Add(r.ptr, resultVsetOff))
	if vsetPtr == nil {
		return 0, false
	}

	numval := *(*int32)(unsafe.Add(vsetPtr, vsetNumvalOff))
	if numval <= 0 {
		return 0, false
	}
	valfmt := *(*int32)(unsafe.Add(vsetPtr, vsetValfmtOff))

	var total float64
	found := false
	for j := range numval {
		pmvPtr := unsafe.Add(vsetPtr, uintptr(vsetVlistOff)+uintptr(j)*sizeofPMValue)

		if wantInsts != nil {
			inst := *(*int32)(unsafe.Add(pmvPtr, pmValueInstOff))
			if !wantInsts[inst] {
				continue
			}
		}

		// pmAtomValue: 8 bytes; we extract as PM_TYPE_DOUBLE → first 8 bytes are the double.
		var atom [8]byte
		rc := _pmExtractValue(valfmt, pmvPtr, desc.metricType, unsafe.Pointer(&atom[0]), pmTypeDouble)
		if rc < 0 {
			continue
		}
		total += *(*float64)(unsafe.Pointer(&atom[0]))
		found = true
	}

	return total, found
}

// ─── sentinel error for end-of-log ──────────────────────────────────────────

var errPMAPIEOL = fmt.Errorf("end of PCP archive log")

// ─── high-level query functions (same signatures as the mockable vars) ──────

func pmapiQuerySamples(_ context.Context, req pcpSamplesRequest) ([]SeriesPoint, error) {
	return withPMAPI(func() ([]SeriesPoint, error) {
		archivePath, err := getCachedArchivePath()
		if err != nil {
			return nil, err
		}

		handle, err := pmapiNewContext(pmContextArchive, archivePath)
		if err != nil {
			return nil, err
		}
		defer _pmDestroyContext(handle)

		pmid, err := pmapiLookupPMID(req.Metric)
		if err != nil {
			return nil, err
		}

		desc, err := pmapiLookupDesc(pmid)
		if err != nil {
			return nil, err
		}

		wantInsts, err := buildInstanceFilter(req.Instances, desc)
		if err != nil {
			return nil, err
		}

		// Determine archive time window, clamping to the actual archive span.
		archiveStart, err := pmapiGetArchiveStart(handle)
		if err != nil {
			return nil, err
		}
		archiveEnd, err := pmapiGetArchiveEnd(handle)
		if err != nil {
			return nil, err
		}

		clampStart := func(t time.Time) time.Time {
			if t.Before(archiveStart) {
				return archiveStart
			}
			return t
		}

		maxSamples := computePCPSampleCount(req.Range, req.ExtraCount)

		if req.NoInterp {
			// Record-driven (non-interpolated) mode.
			// pmlogger may log per-instance metrics (network, disk) at intervals
			// longer than the requested step (e.g. 60s vs 5s). Read further back
			// to collect enough raw samples for rate calculations.
			lookback := max(req.Range.Duration, 5*time.Minute)
			startTime := clampStart(archiveEnd.Add(-lookback))

			if err := pmapiSetMode(pmModeForw, startTime, 0); err != nil {
				return nil, err
			}

			// pmlogger's default interval for per-instance metrics is ~60s.
			// Cap the loop at the lookback window divided by the minimum expected
			// logging interval so we scan the entire requested window.
			const minLogInterval = 10 * time.Second
			maxRecords := int(lookback/minLogInterval) + 100
			return pmapiFetchPoints(pmid, desc, wantInsts, maxRecords)
		}

		startTime := clampStart(archiveEnd.Add(-req.Range.Duration))

		// Interpolated mode.
		stepMs := int32(req.Range.Step.Milliseconds())
		if err := pmapiSetMode(pmModeInterp, startTime, stepMs); err != nil {
			return nil, err
		}

		return pmapiFetchPoints(pmid, desc, wantInsts, maxSamples)
	})
}

func buildInstanceFilter(instances []string, desc pmapiDesc) (map[int32]bool, error) {
	if len(instances) == 0 || desc.indom == pmIndomNull {
		return nil, nil
	}
	instMap, err := pmapiGetInDomMap(desc.indom, true)
	if err != nil {
		return nil, err
	}
	wantInsts := make(map[int32]bool, len(instances))
	for _, name := range instances {
		if id, ok := instMap[name]; ok {
			wantInsts[id] = true
		}
	}
	return wantInsts, nil
}

func pmapiFetchPoints(pmid uint32, desc pmapiDesc, wantInsts map[int32]bool, n int) ([]SeriesPoint, error) {
	points := make([]SeriesPoint, 0, n)
	for range n {
		result, fetchErr := pmapiFetch(pmid)
		if fetchErr != nil {
			if errors.Is(fetchErr, errPMAPIEOL) {
				break
			}
			return nil, fetchErr
		}
		ts := result.timestamp()
		value, ok := result.extractSum(desc, wantInsts)
		result.free()
		if !ok {
			continue
		}
		points = append(points, SeriesPoint{TS: ts.UnixMilli(), Value: value})
	}
	return points, nil
}

func pmapiQueryLiveMetric(_ context.Context, metric string) (float64, error) {
	return withPMAPI(func() (float64, error) {
		handle, err := pmapiNewContext(pmContextHost, "local:")
		if err != nil {
			return 0, err
		}
		defer _pmDestroyContext(handle)

		pmid, err := pmapiLookupPMID(metric)
		if err != nil {
			return 0, err
		}

		desc, err := pmapiLookupDesc(pmid)
		if err != nil {
			return 0, err
		}

		result, err := pmapiFetch(pmid)
		if err != nil {
			return 0, err
		}
		defer result.free()

		value, ok := result.extractSum(desc, nil)
		if !ok {
			return 0, fmt.Errorf("metric %s returned no values", metric)
		}
		return value, nil
	})
}

func pmapiQueryInstanceNames(_ context.Context, metric string) ([]string, error) {
	return withPMAPI(func() ([]string, error) {
		handle, err := pmapiNewContext(pmContextHost, "local:")
		if err != nil {
			return nil, err
		}
		defer _pmDestroyContext(handle)

		pmid, err := pmapiLookupPMID(metric)
		if err != nil {
			return nil, err
		}

		desc, err := pmapiLookupDesc(pmid)
		if err != nil {
			return nil, err
		}

		if desc.indom == pmIndomNull {
			return nil, nil
		}

		instMap, err := pmapiGetInDomMap(desc.indom, false)
		if err != nil {
			return nil, err
		}

		names := make([]string, 0, len(instMap))
		for name := range instMap {
			names = append(names, name)
		}
		return names, nil
	})
}
