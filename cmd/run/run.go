package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	// "strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmpim/anvil"
	"github.com/tmpim/anvil/nbt"
)

// minX, maxX, minZ, maxZ: [-7268, 7732, -7496, 7504]

func main() {
	minCoord := anvil.Coord{
		X: -7270,
		Z: -7460,
	}

	maxCoord := anvil.Coord{
		X: 7740,
		Z: 7550,
	}

	minRegion := minCoord.Region()
	maxRegion := maxCoord.Region()
	minChunk := minCoord.Chunk()
	maxChunk := maxCoord.Chunk()

	if len(os.Args) < 2 {
		fmt.Println("specify the region folder pls")
		return
	}

	// targetComputerID := -1

	targetComputer := (&nbt.TagHeader{
		TagID: nbt.TagInt,
		Name:  []byte("computerID"),
	}).Bytes()

	/*if len(os.Args) == 3 {
		targetComputerID, _ = strconv.Atoi(os.Args[2])
		if targetComputerID >= 0 {
			log.Println("will be searching for computer ID", targetComputerID)
			targetComputer = nbt.NewIntTag("computerID", targetComputerID).Bytes()
		}
	}*/

	_ = targetComputer

	start := time.Now()

	files, err := ioutil.ReadDir(os.Args[1])
	if err != nil {
		panic(err)
	}

	var regionFiles []string

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".mca" {
			regionFiles = append(regionFiles, filepath.Join(os.Args[1], file.Name()))
		}
	}

	if len(regionFiles) == 0 {
		fmt.Println("no regions found??? did you specify the right dir?")
		os.Exit(1)
	}

	var totalBytes int64
	var totalComp int32

	out := make(chan anvil.ChunkData, 1000)

	computerResults := make(chan FoundComputer, 100)

	go func() {
		defer close(out)

		wg := new(sync.WaitGroup)

		for _, file := range regionFiles {
			rd, err := anvil.OpenRegionFile(file)
			if err != nil {
				log.Printf("failed to open %q: %v\n", file, err)
				continue
			}

			if rd.Region.X > maxRegion.X || rd.Region.Z > maxRegion.Z ||
				rd.Region.X < minRegion.X || rd.Region.Z < minRegion.Z {
				continue
			}

			if err := rd.ReadAllChunks(out); err != nil {
				log.Printf("failed to read %q: %v\n", file, err)
			}

			count := atomic.LoadInt64(&totalBytes)
			log.Println("processed:", file, "bytes:", count)
		}

		_ = wg

		wg.Add(1)
		go func() {
			defer wg.Done()

			for _, file := range regionFiles[len(regionFiles)/2:] {
				rd, err := anvil.OpenRegionFile(file)
				if err != nil {
					log.Printf("failed to open %q: %v\n", file, err)
					continue
				}

				if rd.Region.X > maxRegion.X || rd.Region.Z > maxRegion.Z ||
					rd.Region.X < minRegion.X || rd.Region.Z < minRegion.Z {
					continue
				}

				if err := rd.ReadAllChunks(out); err != nil {
					log.Printf("failed to read %q: %v\n", file, err)
				}

				count := atomic.LoadInt64(&totalBytes)
				log.Println("processed:", file, "bytes:", count)
			}
		}()

		wg.Wait()
	}()

	wg := new(sync.WaitGroup)

	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for chunk := range out {
				if chunk.Chunk.X > maxChunk.X || chunk.Chunk.Z > maxChunk.Z ||
					chunk.Chunk.X < minChunk.X || chunk.Chunk.Z < minChunk.Z {
					continue
				}

				// s2 := time.Now()

				nrd, err := nbt.NewTileEntitiesReader(&chunk)
				if err != nil {
					continue
					// panic(err)
				}

				atomic.AddInt64(&totalBytes, int64(nrd.Len()))

				ok, err := nrd.PossibleTagMatch([][][]byte{{targetComputer}})
				if !ok {
					continue
				}

				if err := nrd.PrepareIndex(nbt.SelectiveIndex{
					nbt.TagHeader{
						TagID: nbt.TagList,
						Name:  []byte("TileEntities"),
					},
				}); err != nil {
					log.Println("error indexing:", err)
					log.Printf("error was in chunk %d %d\n", chunk.Chunk.X, chunk.Chunk.Z)
					// continue
				}

				// fmt.Println("got match!")
				results, err := nrd.MatchTags([][]byte{targetComputer})
				if err != nil {
					log.Println("error parsing:", err)
					continue
				}

				for _, result := range results {
					details := nrd.GetTileEntityDetails(result)
					if details == nil {
						log.Println("No tile entity details")
						continue
					}

					var computerID int
					nrd.SeekTo(result.Pos)
					nrd.ReadImmediate(nbt.TagInt, &computerID)

					computerResults <- FoundComputer{
						ID:          computerID,
						Coord:       details.Location,
						Container:   details.Container,
						Approximate: false,
					}
				}
			}
		}()
	}

	compChan := make(chan struct{})

	go func() {
		defer close(compChan)
		for result := range computerResults {
			totalComp++
			// data, err := json.MarshalIndent(result, "", "    ")
			data, err := json.Marshal(result)
			if err != nil {
				panic(err)
			}
			fmt.Println(string(data))
		}
	}()

	wg.Wait()
	close(computerResults)
	<-compChan

	// var stats runtime.MemStats
	// runtime.ReadMemStats(&stats)
	// fmt.Printf("%+v\n", stats)

	log.Println("took:", time.Since(start))
	log.Println("found:", totalComp, "computers")
}
