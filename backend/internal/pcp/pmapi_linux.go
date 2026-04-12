//go:build linux

package pcp

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	pmContextHost    = 1
	pmContextArchive = 2

	pmModeInterp = 1
	pmModeForw   = 2

	pmTypeDouble = 5

	pmIndomNull = 0xFFFFFFFF

	pmErrInst    = -12360
	pmErrEOL     = -12370
	pmErrInstLog = -12380
)

const (
	resultTimestampOff = 0
	resultNumpmidOff   = 16
	resultVsetOff      = 24

	vsetNumvalOff  = 4
	vsetValfmtOff  = 8
	vsetVlistOff   = 16
	sizeofPMValue  = 16
	pmValueInstOff = 0

	descTypeOff  = 4
	descIndomOff = 8
)

var (
	pmapiOnce sync.Once
	pmapiErr  error

	_pmNewContext      func(ctxType int32, name string) int32
	_pmDestroyContext  func(handle int32) int32
	_pmLookupDesc      func(pmid uint32, desc unsafe.Pointer) int32
	_pmSetMode         func(mode int32, when unsafe.Pointer, delta int32) int32
	_pmFetch           func(numpmid int32, pmidlist unsafe.Pointer, result unsafe.Pointer) int32
	_pmFreeResult      func(result unsafe.Pointer)
	_pmExtractValue    func(valfmt int32, ival unsafe.Pointer, itype int32, oval unsafe.Pointer, otype int32) int32
	_pmGetArchiveEnd   func(tvp unsafe.Pointer) int32
	_pmGetArchiveLabel func(lp unsafe.Pointer) int32

	_pmLookupNameAddr         uintptr
	_pmLookupInDomAddr        uintptr
	_pmLookupInDomArchiveAddr uintptr
	_pmGetInDomAddr           uintptr
	_cFreeAddr                uintptr
)

type MetricDesc struct {
	MetricType int32
	InDom      uint32
}

type SamplePoint struct {
	TS    int64
	Value float64
}

type ArchiveRange struct {
	Duration time.Duration
	Step     time.Duration
}

type ArchiveQuery struct {
	Metric     string
	Instances  []string
	Range      ArchiveRange
	ExtraCount int
	NoInterp   bool
}

type LiveMetricValue struct {
	Value     float64
	Found     bool
	Instances map[string]float64
}

func Ensure() error {
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
	_pmLookupInDomAddr, lookupErr = purego.Dlsym(lib, "pmLookupInDom")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve pmLookupInDom: %w", lookupErr)
	}
	_pmLookupInDomArchiveAddr, lookupErr = purego.Dlsym(lib, "pmLookupInDomArchive")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve pmLookupInDomArchive: %w", lookupErr)
	}
	_pmGetInDomAddr, lookupErr = purego.Dlsym(lib, "pmGetInDom")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve pmGetInDom: %w", lookupErr)
	}
	_cFreeAddr, lookupErr = purego.Dlsym(lib, "free")
	if lookupErr != nil {
		return fmt.Errorf("cannot resolve free: %w", lookupErr)
	}

	return nil
}

func LookupPMID(metric string) (uint32, error) {
	nameBytes := cString(metric)
	namePtr := unsafe.Pointer(&nameBytes[0])
	namePtrPtr := unsafe.Pointer(&namePtr)
	var pmid uint32
	r1, _, _ := purego.SyscallN(_pmLookupNameAddr, 1, uintptr(namePtrPtr), uintptr(unsafe.Pointer(&pmid)))
	runtime.KeepAlive(nameBytes)
	rc := int32(r1)
	if rc < 0 {
		return 0, fmt.Errorf("pmLookupName(%q): %s", metric, errString(rc))
	}
	return pmid, nil
}

func LookupDesc(pmid uint32) (MetricDesc, error) {
	var buf [20]byte
	rc := _pmLookupDesc(pmid, unsafe.Pointer(&buf[0]))
	if rc < 0 {
		return MetricDesc{}, fmt.Errorf("pmLookupDesc: %s", errString(rc))
	}
	return MetricDesc{
		MetricType: *(*int32)(unsafe.Pointer(&buf[descTypeOff])),
		InDom:      *(*uint32)(unsafe.Pointer(&buf[descIndomOff])),
	}, nil
}

func GetInDomMap(indom uint32) (map[string]int32, error) {
	var instlistPtr unsafe.Pointer
	var namelistPtr unsafe.Pointer
	r1, _, _ := purego.SyscallN(
		_pmGetInDomAddr,
		uintptr(indom),
		uintptr(unsafe.Pointer(&instlistPtr)),
		uintptr(unsafe.Pointer(&namelistPtr)),
	)
	rc := int32(r1)
	if rc < 0 {
		return nil, fmt.Errorf("pmGetInDom: %s", errString(rc))
	}

	n := int(rc)
	result := make(map[string]int32, n)
	for i := range n {
		instID := *(*int32)(unsafe.Add(instlistPtr, uintptr(i)*4))
		namePtr := *(*unsafe.Pointer)(unsafe.Add(namelistPtr, uintptr(i)*unsafe.Sizeof(uintptr(0))))
		result[goStringFromPtr(namePtr)] = instID
	}

	if instlistPtr != nil {
		purego.SyscallN(_cFreeAddr, uintptr(instlistPtr))
	}
	if namelistPtr != nil {
		purego.SyscallN(_cFreeAddr, uintptr(namelistPtr))
	}
	return result, nil
}

func QueryLiveMetric(_ context.Context, metric string) (float64, error) {
	if err := Ensure(); err != nil {
		return 0, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handle, err := newContext(pmContextHost, "local:")
	if err != nil {
		return 0, err
	}
	defer _pmDestroyContext(handle)

	pmid, err := LookupPMID(metric)
	if err != nil {
		return 0, err
	}
	desc, err := LookupDesc(pmid)
	if err != nil {
		return 0, err
	}
	result, err := fetchPMIDs([]uint32{pmid})
	if err != nil {
		return 0, err
	}
	defer result.free()

	value, ok := result.extractSumAt(0, desc, nil)
	if !ok {
		return 0, fmt.Errorf("metric %s returned no values", metric)
	}
	return value, nil
}

func QueryInstanceNames(_ context.Context, metric string) ([]string, error) {
	if err := Ensure(); err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handle, err := newContext(pmContextHost, "local:")
	if err != nil {
		return nil, err
	}
	defer _pmDestroyContext(handle)

	pmid, err := LookupPMID(metric)
	if err != nil {
		return nil, err
	}
	desc, err := LookupDesc(pmid)
	if err != nil {
		return nil, err
	}
	if desc.InDom == pmIndomNull {
		return nil, nil
	}

	instMap, err := GetInDomMap(desc.InDom)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(instMap))
	for name := range instMap {
		names = append(names, name)
	}
	return names, nil
}

func QueryArchiveSamples(_ context.Context, archivePath string, req ArchiveQuery) ([]SamplePoint, error) {
	if err := Ensure(); err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	handle, err := newContext(pmContextArchive, archivePath)
	if err != nil {
		return nil, err
	}
	defer _pmDestroyContext(handle)

	pmid, err := LookupPMID(req.Metric)
	if err != nil {
		return nil, err
	}
	desc, err := LookupDesc(pmid)
	if err != nil {
		return nil, err
	}
	wantInsts, err := buildInstanceFilter(req.Instances, desc)
	if err != nil {
		return nil, err
	}

	archiveStart, err := getArchiveStart()
	if err != nil {
		return nil, err
	}
	archiveEnd, err := getArchiveEnd()
	if err != nil {
		return nil, err
	}

	clampStart := func(t time.Time) time.Time {
		if t.Before(archiveStart) {
			return archiveStart
		}
		return t
	}

	maxSamples := computeSampleCount(req.Range, req.ExtraCount)
	if req.NoInterp {
		lookback := max(req.Range.Duration, 5*time.Minute)
		startTime := clampStart(archiveEnd.Add(-lookback))
		if err := setMode(pmModeForw, startTime, 0); err != nil {
			return nil, err
		}
		const minLogInterval = 10 * time.Second
		maxRecords := int(lookback/minLogInterval) + 100
		return fetchPoints(pmid, desc, wantInsts, maxRecords)
	}

	startTime := clampStart(archiveEnd.Add(-req.Range.Duration))
	stepMs := int32(req.Range.Step.Milliseconds())
	if err := setMode(pmModeInterp, startTime, stepMs); err != nil {
		return nil, err
	}
	return fetchPoints(pmid, desc, wantInsts, maxSamples)
}

func computeSampleCount(def ArchiveRange, extraCount int) int {
	if def.Step <= 0 {
		return 0
	}
	base := int(def.Duration / def.Step)
	if def.Duration%def.Step != 0 {
		base++
	}
	if base < 1 {
		base = 1
	}
	return base + extraCount
}

func newContext(ctxType int32, name string) (int32, error) {
	if err := Ensure(); err != nil {
		return -1, err
	}
	handle := _pmNewContext(ctxType, name)
	if handle < 0 {
		return -1, fmt.Errorf("pmNewContext(%d, %q): %s", ctxType, name, errString(handle))
	}
	return handle, nil
}

func setMode(mode int32, when time.Time, deltaMs int32) error {
	sec := when.Unix()
	usec := int64(when.Nanosecond() / 1000)
	var tv [16]byte
	*(*int64)(unsafe.Pointer(&tv[0])) = sec
	*(*int64)(unsafe.Pointer(&tv[8])) = usec
	rc := _pmSetMode(mode, unsafe.Pointer(&tv[0]), deltaMs)
	if rc < 0 {
		return fmt.Errorf("pmSetMode: %s", errString(rc))
	}
	return nil
}

func getArchiveEnd() (time.Time, error) {
	var tv [16]byte
	rc := _pmGetArchiveEnd(unsafe.Pointer(&tv[0]))
	if rc < 0 {
		return time.Time{}, fmt.Errorf("pmGetArchiveEnd: %s", errString(rc))
	}
	sec := *(*int64)(unsafe.Pointer(&tv[0]))
	usec := *(*int64)(unsafe.Pointer(&tv[8]))
	return time.Unix(sec, usec*1000), nil
}

func getArchiveStart() (time.Time, error) {
	var label [128]byte
	rc := _pmGetArchiveLabel(unsafe.Pointer(&label[0]))
	if rc < 0 {
		return time.Time{}, fmt.Errorf("pmGetArchiveLabel: %s", errString(rc))
	}
	sec := *(*int64)(unsafe.Pointer(&label[8]))
	usec := *(*int64)(unsafe.Pointer(&label[16]))
	return time.Unix(sec, usec*1000), nil
}

func fetchPoints(pmid uint32, desc MetricDesc, wantInsts map[int32]bool, n int) ([]SamplePoint, error) {
	points := make([]SamplePoint, 0, n)
	for range n {
		result, err := fetchPMIDs([]uint32{pmid})
		if err != nil {
			if errors.Is(err, errPMAPIEOL) {
				break
			}
			return nil, err
		}
		ts := result.timestamp()
		value, ok := result.extractSumAt(0, desc, wantInsts)
		result.free()
		if !ok {
			continue
		}
		points = append(points, SamplePoint{TS: ts.UnixMilli(), Value: value})
	}
	return points, nil
}

func buildInstanceFilter(instances []string, desc MetricDesc) (map[int32]bool, error) {
	if len(instances) == 0 || desc.InDom == pmIndomNull {
		return nil, nil
	}
	wantInsts := make(map[int32]bool, len(instances))
	for _, name := range instances {
		id, ok, err := lookupInDom(desc.InDom, name, true)
		if err != nil {
			return nil, err
		}
		if ok {
			wantInsts[id] = true
		}
	}
	return wantInsts, nil
}

var errPMAPIEOL = fmt.Errorf("end of PCP archive log")

type resultReader struct {
	ptr unsafe.Pointer
}

func fetchPMIDs(pmids []uint32) (*resultReader, error) {
	if len(pmids) == 0 {
		return nil, fmt.Errorf("no PMIDs requested")
	}
	var resultPtr unsafe.Pointer
	rc := _pmFetch(int32(len(pmids)), unsafe.Pointer(&pmids[0]), unsafe.Pointer(&resultPtr))
	if rc < 0 {
		if rc == int32(pmErrEOL) {
			return nil, errPMAPIEOL
		}
		return nil, fmt.Errorf("pmFetch: %s", errString(rc))
	}
	return &resultReader{ptr: resultPtr}, nil
}

func (r *resultReader) free() {
	if r.ptr != nil {
		_pmFreeResult(r.ptr)
		r.ptr = nil
	}
}

func (r *resultReader) timestamp() time.Time {
	sec := *(*int64)(unsafe.Add(r.ptr, resultTimestampOff))
	usec := *(*int64)(unsafe.Add(r.ptr, resultTimestampOff+8))
	return time.Unix(sec, usec*1000)
}

func (r *resultReader) vsetAt(index int) unsafe.Pointer {
	numpmid := int(*(*int32)(unsafe.Add(r.ptr, resultNumpmidOff)))
	if index < 0 || index >= numpmid {
		return nil
	}
	ptrSize := unsafe.Sizeof(uintptr(0))
	return *(*unsafe.Pointer)(unsafe.Add(r.ptr, uintptr(resultVsetOff)+uintptr(index)*ptrSize))
}

func (r *resultReader) extractSumAt(index int, desc MetricDesc, wantInsts map[int32]bool) (float64, bool) {
	vsetPtr := r.vsetAt(index)
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
		inst := *(*int32)(unsafe.Add(pmvPtr, pmValueInstOff))
		if wantInsts != nil && !wantInsts[inst] {
			continue
		}

		value, ok := extractDoubleValue(valfmt, pmvPtr, desc.MetricType)
		if !ok {
			continue
		}
		total += value
		found = true
	}
	return total, found
}

func (r *resultReader) extractInstancesAt(
	index int,
	desc MetricDesc,
	wantInsts map[int32]bool,
	instNames map[int32]string,
) (map[string]float64, bool) {
	vsetPtr := r.vsetAt(index)
	if vsetPtr == nil {
		return nil, false
	}

	numval := *(*int32)(unsafe.Add(vsetPtr, vsetNumvalOff))
	if numval <= 0 {
		return nil, false
	}
	valfmt := *(*int32)(unsafe.Add(vsetPtr, vsetValfmtOff))

	values := make(map[string]float64, numval)
	found := false
	for j := range numval {
		pmvPtr := unsafe.Add(vsetPtr, uintptr(vsetVlistOff)+uintptr(j)*sizeofPMValue)
		inst := *(*int32)(unsafe.Add(pmvPtr, pmValueInstOff))
		if wantInsts != nil && !wantInsts[inst] {
			continue
		}
		value, ok := extractDoubleValue(valfmt, pmvPtr, desc.MetricType)
		if !ok {
			continue
		}

		name := fmt.Sprintf("%d", inst)
		if instNames != nil {
			if resolved, ok := instNames[inst]; ok && strings.TrimSpace(resolved) != "" {
				name = resolved
			}
		}
		values[name] = value
		found = true
	}
	return values, found
}

func extractDoubleValue(valfmt int32, pmvPtr unsafe.Pointer, metricType int32) (float64, bool) {
	var atom [8]byte
	rc := _pmExtractValue(valfmt, pmvPtr, metricType, unsafe.Pointer(&atom[0]), pmTypeDouble)
	if rc < 0 {
		return 0, false
	}
	return *(*float64)(unsafe.Pointer(&atom[0])), true
}

func lookupInDom(indom uint32, name string, archive bool) (int32, bool, error) {
	sym := _pmLookupInDomAddr
	label := "pmLookupInDom"
	if archive {
		sym = _pmLookupInDomArchiveAddr
		label = "pmLookupInDomArchive"
	}

	nameBytes := cString(name)
	r1, _, _ := purego.SyscallN(sym, uintptr(indom), uintptr(unsafe.Pointer(&nameBytes[0])))
	runtime.KeepAlive(nameBytes)
	rc := int32(r1)
	switch rc {
	case pmErrInst, pmErrInstLog:
		return 0, false, nil
	}
	if rc < 0 {
		return 0, false, fmt.Errorf("%s(%q): %s", label, name, errString(rc))
	}
	return rc, true, nil
}

func cString(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

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

func errString(rc int32) string {
	return fmt.Sprintf("PMAPI error %d", rc)
}
