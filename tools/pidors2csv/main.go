package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"leaguewatcher/internal/leaguewatcher/bot/repository"
	"log"
	"os"
	"strconv"
)

func main() {
	fd, err := os.Open("pidors.json")
	if err != nil {
		log.Fatal(err)
	}
	defer fd.Close()

	data, err := io.ReadAll(fd)
	if err != nil {
		log.Fatal(err)
	}

	var pidors repository.Pidor
	err = json.Unmarshal(data, &pidors)
	if err != nil {
		log.Fatal(err)
	}

	for chid, ch := range pidors.Stats {
		err = func() error {
			fd, err := os.Create(fmt.Sprintf("%s.csv", chid))
			if err != nil {
				return err
			}
			defer fd.Close()

			w := csv.NewWriter(fd)

			for _, stat := range ch {
				err = w.Write([]string{stat.Name, strconv.Itoa(stat.Count)})
				if err != nil {
					return err
				}
			}
			w.Flush()
			if err := w.Error(); err != nil {
				return err
			}

			return nil
		}()
		if err != nil {
			log.Fatal(err)
		}
	}
}
