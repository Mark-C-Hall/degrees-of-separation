package main

import (
	"context"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/graph"
	"github.com/mark-c-hall/degrees-of-separation/internal/tmdb"
)

var pagesFlag = flag.Int("pages", 100, "number of movie api pages to consume (around 20 results per page)")
var maxCastFlag = flag.Int("max-cast", 20, "top k billed actors from a movie")
var resumeFlag = flag.Bool("resume", false, "bool to resume from previous movie api page")
var allFlag = flag.Bool("all", false, "consume all available pages (overrides -pages)")

func main() {
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalln("Error loading config:", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := tmdb.NewClient(*cfg)

	db, err := graph.NewDriver(ctx, *cfg)
	if err != nil {
		log.Fatalln("Error connecting to neo4j:", err)
	}
	defer db.Close(context.Background())

	firstPage := 1
	if *resumeFlag {
		lastPage, err := db.GetLastIngestedPage(ctx)
		if err != nil {
			log.Fatalln("Error reading last ingested page:", err)
		}
		firstPage = lastPage + 1
		log.Printf("Resuming from page %d", firstPage)
	}

	lastPage := *pagesFlag
	if *allFlag {
		lastPage = math.MaxInt
	}
	if firstPage > lastPage {
		log.Printf("Nothing to do: first page %d > last page %d", firstPage, lastPage)
		return
	}

	for page := firstPage; page <= lastPage; page++ {
		if ctx.Err() != nil {
			log.Println("Interrupted, stopping ingest")
			break
		}

		totalPages, movies, err := client.GetPopularMovies(ctx, page)
		if err != nil {
			log.Printf("Error fetching popular movies page %d, skipping: %v", page, err)
			continue
		}
		if totalPages < lastPage {
			lastPage = totalPages
		}

		log.Printf("Processing page %d/%d", page, lastPage)

		for i, movie := range movies {
			log.Printf("  Movie %d/%d: %q (%d)", i+1, len(movies), movie.Title, movie.Year)

			cast, err := client.GetMovieCast(ctx, movie.TmdbID, *maxCastFlag)
			if err != nil {
				if ctx.Err() != nil {
					break
				}
				log.Printf("Error fetching cast for %q (tmdb=%d), skipping: %v", movie.Title, movie.TmdbID, err)
				continue
			}

			log.Printf("    Ingesting %d actors and costar edges", len(cast))

			if err := db.IngestMovieCast(ctx, movie, cast); err != nil {
				if ctx.Err() != nil {
					break
				}
				log.Printf("Error ingesting cast for %q: %v", movie.Title, err)
			}
		}

		if ctx.Err() == nil {
			if err := db.SetLastIngestedPage(ctx, page); err != nil {
				log.Printf("Error saving ingest state for page %d: %v", page, err)
			}
		}
	}

	log.Println("Ingest complete")
}
