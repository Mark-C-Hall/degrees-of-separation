package models

type Actor struct {
	TmdbID int
	Name   string
}

type Movie struct {
	TmdbID int
	Title  string
	Year   int
}
