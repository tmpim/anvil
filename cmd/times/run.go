package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
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

			// log.Println("processed:", file)
		}
	}()

	wg := new(sync.WaitGroup)
	statsMutex := new(sync.Mutex)
	stats := make(map[string]int)

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

				nrd, err := nbt.NewRegionChunkReader(&chunk)
				if err != nil {
					continue
					// panic(err)
				}

				results := nrd.SimpleMatch([]byte("The Transreich Trade Agreement"), -1)
				if len(results) == 0 {
					continue
				}

				err = nrd.PrepareIndex(nbt.SelectiveIndex{
					nbt.TagHeader{
						TagID: nbt.TagList,
						Name:  []byte("TileEntities"),
					},
				})
				if err != nil {
					panic(err)
				}

				for _, res := range results {
					nrd.SeekTo(res)
					idx := nrd.AlignToIndex()
					if idx == nil {
						log.Println("got nil index, skipping...")
						continue
					}

					ent := nrd.GetTileEntityDetails(idx)
					if ent.Location.Dist(&anvil.Coord{
						X: 235,
						Y: 25,
						Z: 73,
					}) < 10 {
						continue
					}

					nrd.SeekTo(idx.Pos)

					if idx.Header.TagID == nbt.TagString {
						var title string
						nrd.ReadImmediate(nbt.TagString, &title)
						fmt.Println("title:", title)

						statsMutex.Lock()
						stats[title]++
						statsMutex.Unlock()
					}

					fmt.Printf("%+v\n", *ent)
				}
			}
		}()
	}

	wg.Wait()

	log.Println("took:", time.Since(start))

	for k, v := range stats {
		fmt.Println(k, ":", v)
	}
}
