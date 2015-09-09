package main

import (
	// "bytes"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	goflac "github.com/cocoonlife/goflac"
	"github.com/padster/go-sound/cq"
)

// Generates the golden files. See test/sounds_test.go for actual test.
func main() {
	// Singlethreaded for now...
	runtime.GOMAXPROCS(1)

	fmt.Printf("Parsing flags\n")
	minFreq := flag.Float64("minFreq", 110.0, "minimum frequency")
	maxFreq := flag.Float64("maxFreq", 14080.0, "maximum frequency")
	bpo := flag.Int("bpo", 24, "Buckets per octave")
	flag.Parse()
	remainingArgs := flag.Args()

	if len(remainingArgs) != 2 {
		panic("Required: <input> <output> filename arguments")
	}
	inputFile := remainingArgs[0]
	outputFile := remainingArgs[1]

	fmt.Printf("Building parameters\n")

	sampleRate := 44100.0
	// minFreq, maxFreq, bpo := 110.0, 14080.0, 24
	params := cq.NewCQParams(sampleRate, *minFreq, *maxFreq, *bpo)

	fmt.Printf("Params = %v\n", params)
	fmt.Printf("Building CQ and inverse... %v\n", params)

	constantQ := cq.NewConstantQ(params)
	cqInverse := cq.NewCQInverse(params)

	fmt.Printf("Loading file from %v\n", inputFile)
	if !strings.HasSuffix(inputFile, ".flac") {
		panic("Input file must be .flac")
	}

	fileReader, err := goflac.NewDecoder(inputFile)
	if err != nil {
		fmt.Printf("Error reading file! %v\n", err)
		panic("Can't read file")
	}
	defer fileReader.Close()

	fileWriter, err := goflac.NewEncoder(outputFile, 1, 24, int(sampleRate))
	if err != nil {
		fmt.Printf("Error opening file to write to! %v\n", err)
		panic("Can't write file")
	}
	defer fileWriter.Close()

	inframe := 0
	outframe := 0
	latency := constantQ.OutputLatency + cqInverse.OutputLatency

	fmt.Printf("forward latency = %d, inverse latency = %d, total = %d\n", constantQ.OutputLatency, cqInverse.OutputLatency, latency)

	startTime := time.Now()

	frame, err := fileReader.ReadFrame()
	frameAt := 0
	for err != io.EOF {
		if frame.Depth != 24 {
			fmt.Printf("Only depth 24-bit flac supported for now, file is %d. TODO: support more...", frame.Depth)
			panic("Unsupported input file format")
		}
		if frame.Rate != 44100 {
			fmt.Printf("Only 44.1khz flac supported for now, file is %d. TODO: support more...", frame.Rate)
			panic("Unsupported input file format")
		}

		count := len(frame.Buffer) / frame.Channels
		samples := make([]float64, count, count)
		total := 0.0
		for i := 0; i < count; i++ {
			v := 0.0
			for _, c := range frame.Buffer[i*frame.Channels : (i+1)*frame.Channels] {
				v += floatFrom24bit(c)
			}
			v = v / float64(frame.Channels)
			samples[i] = v
			total += v
		}

		result := constantQ.Process(samples)
		cqout := cqInverse.Process(result)

		if outframe >= latency {
			writeFrame(fileWriter, cqout)

		} else if outframe+len(cqout) >= latency {
			offset := latency - outframe
			writeFrame(fileWriter, cqout[offset:])
		}

		frameAt++
		inframe += count
		outframe += len(cqout)
		frame, err = fileReader.ReadFrame()
	}

	r := append(
		cqInverse.Process(constantQ.GetRemainingOutput()),
		cqInverse.GetRemainingOutput()...)

	writeFrame(fileWriter, r)
	outframe += len(r)

	fmt.Printf("in: %d, out: %d\n", inframe, outframe-latency)

	elapsedSeconds := time.Since(startTime).Seconds()
	fmt.Printf("elapsed time (not counting init): %f sec, frames/sec = %f\n",
		elapsedSeconds, float64(inframe)/elapsedSeconds)
}

func floatFrom24bit(input int32) float64 {
	return float64(input) / (float64(1<<23) - 1.0) // Hmmm..doesn't seem right?
}
func int24FromFloat(input float64) int32 {
	return int32(input * (float64(1<<23) - 1.0))
}

func writeFrame(file *goflac.Encoder, samples []float64) { // samples in range [-1, 1]
	n := len(samples)
	frame := goflac.Frame{
		1,     /* channels */
		24,    /* depth */
		44100, /* rate */
		make([]int32, n, n),
	}
	for i, v := range samples {
		frame.Buffer[i] = int24FromFloat(v)
	}
	if err := file.WriteFrame(frame); err != nil {
		fmt.Printf("Error writing frame to file :( %v\n", err)
		panic("Can't write to file")
	}
}
