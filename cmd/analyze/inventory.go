package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmpim/anvil/nbt"
)

type PlayerFile struct {
	Player string
	Data   []byte
}

type PlayerComputer struct {
	ComputerID int
	Player     string
}

// minX, maxX, minZ, maxZ: [-7268, 7732, -7496, 7504]

func main() {
	if len(os.Args) < 2 {
		log.Println("specify the player data folder pls")
		return
	}

	start := time.Now()

	files, err := ioutil.ReadDir(os.Args[1])
	if err != nil {
		panic(err)
	}

	var playerFiles []string

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".dat" {
			playerFiles = append(playerFiles, filepath.Join(os.Args[1], file.Name()))
		}
	}

	if len(playerFiles) == 0 {
		fmt.Println("no players found??? did you specify the right dir?")
		os.Exit(1)
	}

	var totalBytes int64
	var totalComp int32

	out := make(chan PlayerFile, 10)
	computerResults := make(chan PlayerComputer, 100)

	go func() {
		defer close(out)

		for _, file := range playerFiles {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				log.Printf("failed to read player file %q: %v", file, err)
				continue
			}

			uuid := strings.Split(filepath.Base(file), ".")[0]

			out <- PlayerFile{
				Player: uuid,
				Data:   data,
			}
		}
	}()

	wg := new(sync.WaitGroup)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for playerfile := range out {
				nrd, err := nbt.NewGzipReader(bytes.NewReader(playerfile.Data))
				if err != nil {
					log.Printf("failed to ungzip player file %q: %v\n", playerfile.Player, err)
					continue
				}

				atomic.AddInt64(&totalBytes, int64(nrd.Len()))

				ok, err := nrd.PossibleTagMatch([][][]byte{
					{
						// (&nbt.TagHeader{
						// 	TagID: nbt.TagCompound,
						// 	Name:  []byte("TileEntities"),
						// }).Bytes(),
						(&nbt.TagHeader{
							TagID: nbt.TagInt,
							Name:  []byte("computerID"),
						}).Bytes(),
						// nbt.NewIntTag("computerID", 0).Bytes(),
					},
				})

				if !ok {
					continue
				}

				if err := nrd.PrepareIndex(nil); err != nil {
					log.Println("error indexing:", err)
					continue
				}

				fmt.Println(string(nrd.StructureToJSON(nrd.Index[0])))

				// fmt.Println("got match!")
				results, err := nrd.MatchTags([][]byte{
					(&nbt.TagHeader{
						TagID: nbt.TagInt,
						Name:  []byte("computerID"),
					}).Bytes(),
					// nbt.NewIntTag("computerID", 0).Bytes(),
				})
				if err != nil {
					log.Println("error parsing:", err)
					continue
				}

				for _, result := range results {
					var computerID int
					nrd.SeekTo(result.Pos)
					nrd.ReadImmediate(nbt.TagInt, &computerID)

					computerResults <- PlayerComputer{
						ComputerID: computerID,
						Player:     playerfile.Player,
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
