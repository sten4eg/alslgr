package alslgr

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	ForcedErrorMessage = "FORCED ERROR"
)

var (
	forcedError = errors.New(ForcedErrorMessage)
)

type (
	TestDumper bytes.Buffer
)

func (d *TestDumper) Dump(b []byte) error {
	if string(b) == ForcedErrorMessage {
		return forcedError
	} else {
		_, err := (*bytes.Buffer)(d).Write(b)
		return err
	}
}

type (
	Testcase struct {
		Name                       string
		Cap                        int
		Dumper                     Dumper
		Data                       [][]byte
		DumpedDataBeforeManualDump []byte
		ExpectedConstructorErr     error
		ExpectedWriteErrs          []error
		ExpectedDumpErr            error
	}
)

func PrepareTests() []Testcase {
	return []Testcase{
		{
			Name:   "CAP 1 DATA 'A'",
			Cap:    1,
			Dumper: &TestDumper{},
			Data: [][]byte{
				[]byte("A"),
			},
			DumpedDataBeforeManualDump: []byte{},
			ExpectedConstructorErr:     nil,
			ExpectedWriteErrs:          []error{nil},
			ExpectedDumpErr:            nil,
		},
		{
			Name:   "CAP 1 DATA 'AA'",
			Cap:    1,
			Dumper: &TestDumper{},
			Data: [][]byte{
				[]byte("AA"),
			},
			DumpedDataBeforeManualDump: []byte("AA"),
			ExpectedConstructorErr:     nil,
			ExpectedWriteErrs:          []error{nil},
			ExpectedDumpErr:            nil,
		},
		{
			Name:   "CAP 2 DATA 'A'+'A'+'A'",
			Cap:    2,
			Dumper: &TestDumper{},
			Data: [][]byte{
				[]byte("A"), []byte("A"), []byte("A"),
			},
			DumpedDataBeforeManualDump: []byte("AA"),
			ExpectedConstructorErr:     nil,
			ExpectedWriteErrs:          []error{nil, nil, nil},
			ExpectedDumpErr:            nil,
		},
		{
			Name:   "CAP 2 DATA 'A'+'A'+'A'+'A'",
			Cap:    2,
			Dumper: &TestDumper{},
			Data: [][]byte{
				[]byte("A"), []byte("A"), []byte("A"), []byte("A"),
			},
			DumpedDataBeforeManualDump: []byte("AA"),
			ExpectedConstructorErr:     nil,
			ExpectedWriteErrs:          []error{nil, nil, nil, nil},
			ExpectedDumpErr:            nil,
		},
		{
			Name:   "DUMPER FORCED ERROR",
			Cap:    12,
			Dumper: &TestDumper{},
			Data: [][]byte{
				[]byte(ForcedErrorMessage), []byte("A"),
			},
			DumpedDataBeforeManualDump: []byte{},
			ExpectedConstructorErr:     nil,
			ExpectedWriteErrs:          []error{nil, forcedError},
			ExpectedDumpErr:            forcedError,
		},
	}
}

func mergeBytes(bs [][]byte) []byte {
	var length int

	for _, b := range bs {
		length += len(b)
	}

	result := make([]byte, 0, length)

	for _, b := range bs {
		result = append(result, b...)
	}

	return result
}

func TestLogger(t *testing.T) {
	tests := PrepareTests()

	for _, test := range tests {
		l := NewLogger(test.Cap, test.Dumper)

		for i, data := range test.Data {
			_, err := l.Write(data)
			if !errors.Is(err, test.ExpectedWriteErrs[i]) {
				t.Errorf("TEST \"%s\" FAILED: EXPECTED WRITE ERROR \"%v\" GOT \"%v\"\n", test.Name, test.ExpectedWriteErrs[i], err)
			}
		}

		dataBeforeDump := (*bytes.Buffer)(test.Dumper.(*TestDumper)).Bytes()

		if !bytes.Equal(dataBeforeDump, test.DumpedDataBeforeManualDump) {
			t.Errorf("TEST \"%s\" FAILED: EXPECTED DATA BEFORE MANUAL DUMP \"%s\" GOT \"%s\"\n",
				test.Name, test.DumpedDataBeforeManualDump, dataBeforeDump)
		}

		err := l.DumpBuffer()
		if !errors.Is(err, test.ExpectedDumpErr) {
			t.Errorf("TEST \"%s\" FAILED: EXPECTED DUMP ERROR \"%v\" GOT %v\n", test.Name, test.ExpectedDumpErr, err)
		}
		if err != nil {
			continue
		}

		givenResult := (*bytes.Buffer)(test.Dumper.(*TestDumper)).Bytes()
		expectedResult := mergeBytes(test.Data)
		if !bytes.Equal(givenResult, expectedResult) {
			t.Errorf("TEST \"%s\" FAILED: EXPECTED DATA AFTER MANUAL DUMP %s GOT %s\n",
				test.Name, expectedResult, givenResult)
		}
	}
}

const (
	AutoDumpTestDelay = time.Millisecond * 300
)

func TestAutoDump(t *testing.T) {
	d := &TestDumper{}
	l := NewLogger(1<<3, d)

	_, err := l.Write([]byte("A"))
	if err != nil {
		t.Errorf("TEST \"AUTO DUMP\" FAILED: EXPECTED WRITE ERROR \"nil\" GOT \"%v\"\n", err)
	}

	errCh, cancel := l.AutoDumpBuffer(AutoDumpTestDelay)

	go func() {
		for dumpErr := range errCh {
			if dumpErr != nil {
				t.Errorf("TEST \"AUTO DUMP\" FAILED: EXPECTED ASYNC DUMP ERROR \"nil\" GOT \"%v\"\n", dumpErr)
			}
		}
	}()

	_, err = l.Write([]byte("A"))
	if err != nil {
		t.Errorf("TEST \"AUTO DUMP\" FAILED: EXPECTED WRITE ERROR \"nil\" GOT \"%v\"\n", err)
	}
	time.Sleep(AutoDumpTestDelay)

	_, err = l.Write([]byte("A"))
	if err != nil {
		t.Errorf("TEST \"AUTO DUMP\" FAILED: EXPECTED WRITE ERROR \"nil\" GOT \"%v\"\n", err)
	}
	time.Sleep(AutoDumpTestDelay * 2)

	cancel()

	givenResult := string((*bytes.Buffer)(d).Bytes())
	if givenResult != "AAA" {
		t.Errorf("TEST \"AUTO DUMP\" FAILED: EXPECTED DATA %s GOT %s\n", "AAA", givenResult)
	}
}

const (
	Concurrency = 10000
)

func TestConcurrentWrite(t *testing.T) {
	d := &TestDumper{}
	l := NewLogger(1<<12, d)

	var wg sync.WaitGroup
	for i := 0; i < Concurrency; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, i int, l Logger, t *testing.T) {
			defer wg.Done()

			_, err := l.Write([]byte(fmt.Sprintf("%d| GOROUTINE WRITE\n", i)))
			if err != nil {
				t.Errorf("TEST \"CONCURRENT WRITE\" FAILED: EXPECTED WRITE ERROR \"nil\" GOT \"%v\"\n", err)
			}
		}(&wg, i, l, t)
	}

	wg.Wait()

	err := l.DumpBuffer()
	if err != nil {
		t.Errorf("TEST \"CONCURRENT WRITE\" FAILED: EXPECTED DUMP ERROR \"nil\" GOT \"%v\"\n", err)
		return
	}

	buf := (*bytes.Buffer)(d)

	validate := regexp.MustCompile("^[0-9]+| GOROUTINE WRITE$").MatchString

	checkArr := [Concurrency]bool{}
	for {
		var s string
		s, err = buf.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			} else {
				t.Errorf("TEST \"CONCURRENT WRITE\" FAILED: EXPECTED READ ERROR \"nil\" GOT \"%v\"\n", err)
				return
			}
		}

		if !validate(s) {
			t.Errorf("TEST \"CONCURRENT WRITE\" FAILED: GOT INVALID ROW \"%s\"\n", s)
			continue
		}

		numStr, _, _ := strings.Cut(s, "|")

		var number int64
		number, err = strconv.ParseInt(numStr, 10, 64)
		if err != nil {
			t.Errorf("TEST \"CONCURRENT WRITE\" FAILED: EXPECTED NUMBER CONVERTION ERROR \"nil\" GOT \"%v\"\n", err)
			continue
		}

		checkArr[number] = true
	}

	for i := range checkArr {
		if !checkArr[i] {
			t.Errorf("TEST \"CONCURRENT WRITE\" FAILED: LOST ROW \"%d\"\n", i)
		}
	}
}
