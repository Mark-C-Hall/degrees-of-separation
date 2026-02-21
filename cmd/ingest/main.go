package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mark-c-hall/degrees-of-separation/internal/config"
	"github.com/mark-c-hall/degrees-of-separation/internal/tmdb"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalln("Error loading config:", err)
	}

	client := tmdb.NewClient(*cfg)

	set := make(map[int]struct{})

	ctx := context.Background()
	totalMovies := 0
	for page := 1; page <= 10; page++ {
		movies, err := client.GetPopularMovies(ctx, page)
		if err != nil {
			log.Fatalln("Error fetching popular movies:", err)
		}
		for _, movie := range movies.Results {
			totalMovies++
			cast, err := client.GetMovieCast(ctx, movie.ID, 20)
			if err != nil {
				log.Fatalln("Error fetching movie cast:", err)
			}
			for _, actor := range cast {
				set[actor.ID] = struct{}{}
			}
		}
	}

	fmt.Println("Total Movies Processed:", totalMovies)
	fmt.Println("Unique Actors:", len(set))
}
