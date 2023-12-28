package main

import (
	"flag"
	"fmt"
	"github.com/gabriel-vasile/mimetype"
	"github.com/rwcarlsen/goexif/exif"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type processImageArgs struct {
	fileName string
	filePath string
	outDir   string
}

type errorList struct {
	errs []error
}

func (e *errorList) add(err error) {
	if err != nil {
		e.errs = append(e.errs, err)
	}
}

func (e *errorList) hasError() bool {
	return len(e.errs) > 0
}

func (e *errorList) Error() string {
	if !e.hasError() {
		return ""
	}
	var msgs []string
	for _, err := range e.errs {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "\n")
}

func processImageAsync(wg *sync.WaitGroup, processImageArgsChan chan processImageArgs, errChan chan error) {
	defer wg.Done()
	for args := range processImageArgsChan {
		err := processImage(args)
		if err != nil {
			errChan <- err
		}
	}
}

func processImage(args processImageArgs) error {
	f, err := os.Open(args.filePath)
	if err != nil {
		return fmt.Errorf("failed to open file '%s': %w", args.fileName, err)
	}
	defer f.Close()

	var mimeType *mimetype.MIME
	mimeType, err = mimetype.DetectReader(f)
	if err != nil {
		return fmt.Errorf("failed to read MIME type from '%s': %w", args.fileName, err)
	}

	_, _ = f.Seek(0, io.SeekStart)
	if !mimeType.Is("image/jpeg") {
		return fmt.Errorf("ignore '%s' because of unsupported MIME type '%s'", args.fileName, mimeType)
	}

	var x *exif.Exif
	x, err = exif.Decode(f)
	if err != nil {
		return fmt.Errorf("failed to read EXIF data from '%s': %w", args.fileName, err)
	}

	var tm time.Time
	tm, err = x.DateTime()
	if err != nil {
		return fmt.Errorf("failed to get time from EXIF data in '%s': %w", args.fileName, err)
	}

	outDir := filepath.Join(args.outDir, tm.Month().String())
	err = os.MkdirAll(outDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory '%s': %w", outDir, err)
	}

	outPath := filepath.Join(outDir, args.fileName)
	err = os.Rename(args.filePath, outPath)
	if err != nil {
		return fmt.Errorf("failed to move '%s' to '%s'", args.fileName, outDir)
	}

	_, _ = fmt.Fprintf(os.Stdout, "Moved '%s' to '%s' ...\n", args.fileName, outDir)

	return nil
}

func init() {}

func main() {
	var (
		flagInDir  *string = flag.String("in", "./", "The input directory")
		flagOutDir *string = flag.String("out", "", "The output directory")
	)

	flag.Parse()

	if *flagOutDir == "" {
		_, _ = fmt.Fprintln(os.Stderr, "the output directory is missing")
		os.Exit(1)
	}

	outDir, _ := filepath.Abs(*flagOutDir)
	numCPU := runtime.NumCPU()
	wg := &sync.WaitGroup{}
	errList := &errorList{}
	processImageChan := make(chan processImageArgs)
	errChan := make(chan error)

	wg.Add(numCPU)
	for i := 0; i < numCPU; i++ {
		go processImageAsync(wg, processImageChan, errChan)
	}
	go func(errList *errorList, errChan chan error) {
		for err := range errChan {
			errList.add(err)
		}
	}(errList, errChan)

	dir, err := ioutil.ReadDir(*flagInDir)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, fmt.Errorf("failed to reading directory '%s': %w", *flagInDir, err))
		os.Exit(1)
	}

	for _, fi := range dir {
		if fi.IsDir() {
			continue
		}
		filePath, _ := filepath.Abs(filepath.Join(*flagInDir, fi.Name()))
		processImageChan <- processImageArgs{fi.Name(), filePath, outDir}
	}

	close(processImageChan)
	close(errChan)
	wg.Wait()

	if errList.hasError() {
		_, _ = fmt.Fprintln(os.Stderr, errList.Error())
	}
}
