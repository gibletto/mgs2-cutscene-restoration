// sdtinspect dumps the structure of a .sdt cutscene: chunk histogram, packet
// histogram, camera track summary, effect usage, and terminator sanity.
package main

import (
	"fmt"
	"os"
	"sort"

	"mgs2-restoration/cutscene-studio/internal/sdt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sdtinspect <file.sdt>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	s, err := sdt.Decode(raw)
	if err != nil {
		panic(err)
	}

	chunkHist := map[string]int{}
	pcmBytes := 0
	frames := 0
	pktHist := map[string]int{}
	effHist := map[[2]int32]int{}
	termSeqAt := []int{}
	var firstCam, lastCam sdt.CameraFrame

	for i := range s.Chunks {
		c := &s.Chunks[i]
		chunkHist[c.Type().String()]++
		switch c.Type() {
		case sdt.CPCM:
			pcmBytes += len(c.Payload)
		case sdt.CDemo:
			fr, err := sdt.DecodeFrame(c.Payload)
			if err != nil {
				panic(err)
			}
			for j := range fr.Packets {
				p := &fr.Packets[j]
				pktHist[p.Kind().String()]++
				if p.Kind() == sdt.PTermSeq {
					termSeqAt = append(termSeqAt, frames)
				}
				if e, ok := p.Effect(); ok {
					effHist[[2]int32{e.ComposerID(), e.ExecType}]++
				}
				if cam, ok := p.Camera(); ok {
					if frames == 0 {
						firstCam = cam
					}
					lastCam = cam
				}
			}
			frames++
		}
	}

	fmt.Printf("file        %s (%d bytes)\n", os.Args[1], len(raw))
	fmt.Printf("chunks      %d  %v\n", len(s.Chunks), chunkHist)
	fmt.Printf("frames      %d  (~%.1fs at 60fps)\n", frames, float64(frames)/60)
	fmt.Printf("pcm         %d bytes\n", pcmBytes)
	fmt.Printf("tail        %d bytes\n", len(s.Tail))
	fmt.Printf("term_seq    at frame(s) %v   (invariant: exactly one, on the last frame %d)\n", termSeqAt, frames-1)
	fmt.Printf("camera[0]   screen=%.3f zrot=%.4f no=%d chanl=%d pos=%v trg=%v\n",
		firstCam.Screen, firstCam.ZRotate, firstCam.No, firstCam.Chanl, firstCam.Pos, firstCam.Trg)
	fmt.Printf("camera[n]   screen=%.3f zrot=%.4f pos=%v trg=%v\n",
		lastCam.Screen, lastCam.ZRotate, lastCam.Pos, lastCam.Trg)

	fmt.Println("packets:")
	keys := make([]string, 0, len(pktHist))
	for k := range pktHist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-12s %6d\n", k, pktHist[k])
	}

	fmt.Println("effects (composer_id, exec_type) x count:")
	ekeys := make([][2]int32, 0, len(effHist))
	for k := range effHist {
		ekeys = append(ekeys, k)
	}
	sort.Slice(ekeys, func(a, b int) bool { return effHist[ekeys[a]] > effHist[ekeys[b]] })
	for _, k := range ekeys {
		fmt.Printf("  0x%05x exec=%d  x%d\n", k[0], k[1], effHist[k])
	}
}
