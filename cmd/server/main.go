package main

import (
	"log"

	"dovakin0007.com/notes-grpc/internal/server"
	"github.com/joho/godotenv"
)

// TODO: replace all the loggers they look ass cuz its ai made

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalln("Error loading .env the server hasn't been started")
	}
	server.CreateAndStartServer()
}
