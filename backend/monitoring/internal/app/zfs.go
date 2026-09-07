package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func ARCSize() (uint64, error) {
	file, err := os.Open("/proc/spl/kstat/zfs/arcstats")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	return arcSizeFromReader(file)
}

func arcSizeFromReader(r io.Reader) (uint64, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || fields[0] != "size" {
			continue
		}
		if len(fields) < 3 {
			return 0, fmt.Errorf("unexpected arcstats size format: %s", scanner.Text())
		}
		return strconv.ParseUint(fields[2], 10, 64)
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return 0, fmt.Errorf("size field not found in arcstats")
}
