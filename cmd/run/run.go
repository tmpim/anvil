package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmpim/anvil"
	"github.com/tmpim/anvil/nbt"
)

// minX, maxX, minZ, maxZ: [-7268, 7732, -7496, 7504]

func main() {
	minCoord := anvil.Coord{
		X: -7268,
		Z: -7496,
	}

	maxCoord := anvil.Coord{
		X: 7732,
		Z: 7504,
	}

	minRegion := minCoord.Region()
	maxRegion := maxCoord.Region()
	minChunk := minCoord.Chunk()
	maxChunk := maxCoord.Chunk()

	if len(os.Args) < 2 {
		fmt.Println("specify the region folder pls")
	}

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

	out := make(chan anvil.ChunkData, 10)

	go func() {
		defer close(out)

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
	}()

	wg := new(sync.WaitGroup)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for chunk := range out {
				if chunk.Chunk.X > maxChunk.X || chunk.Chunk.Z > maxChunk.Z ||
					chunk.Chunk.X < minChunk.X || chunk.Chunk.Z < minChunk.Z {
					continue
				}

				// s2 := time.Now()

				nrd, err := chunk.NBTReader()
				if err != nil {
					continue
					// panic(err)
				}

				atomic.AddInt64(&totalBytes, int64(nrd.Len()))

				ok, err := nrd.PossibleTagMatch([][][]byte{
					{
						// (&nbt.TagHeader{
						// 	TagID: nbt.TagInt,
						// 	Name:  []byte("computerID"),
						// }).Bytes(),
						nbt.NewIntTag("computerID", 0).Bytes(),
					},
				})

				if !ok {
					continue
				}

				nrd.PrepareIndex(nbt.SelectiveIndex{
					nbt.TagHeader{
						TagID: nbt.TagList,
						Name:  []byte("TileEntities"),
					},
				})

				// fmt.Println("got match!")
				results, err := nrd.MatchTags([][]byte{
					// (&nbt.TagHeader{
					// 	TagID: nbt.TagInt,
					// 	Name:  []byte("computerID"),
					// }).Bytes(),
					nbt.NewIntTag("computerID", 0).Bytes(),
				})

				for _, result := range results {
					fmt.Printf("got result, header: %+v, chunk coord: %+v\n", result[1].Header, chunk.Chunk.CornerCoord())
					for _, child := range result[1].Children {
						fmt.Println("key:", string(nrd.Index[child].Header.Name))
					}
					fmt.Println("breadcrumb:", result.String())
				}

				// len(results)

				// _ = results
			}
		}()
	}

	wg.Wait()

	// var stats runtime.MemStats
	// runtime.ReadMemStats(&stats)
	// fmt.Printf("%+v\n", stats)

	fmt.Println("took:", time.Since(start))
	fmt.Println("found:", totalComp, "computers")
}
