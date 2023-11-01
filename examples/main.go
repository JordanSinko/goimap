package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/emersion/go-message/mail"
	"github.com/jordansinko/goimap"
)

func main() {

	p := goimap.MessageFetcherParams{
		Address:  "imap.gmail.com:993",
		Username: "example@gmail.com",
		Password: "MyPassword123",
		OnMessage: func(mr *mail.Reader) {

			// Print some info about the message
			header := mr.Header

			if date, err := header.Date(); err == nil {
				log.Println("Date: ", date)
			}

			if from, err := header.AddressList("From"); err == nil {
				log.Println("From: ", from)
			}

			if to, err := header.AddressList("To"); err == nil {
				log.Println("To: ", to)
			}

			if subject, err := header.Subject(); err == nil {
				log.Println("Subject: ", subject)
			}

			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				} else if err != nil {
					log.Fatal(err)
				}

				switch h := p.Header.(type) {
				case *mail.InlineHeader:
					b, _ := ioutil.ReadAll(p.Body)
					log.Printf("Got text: %v\n", string(b))
				case *mail.AttachmentHeader:
					filename, _ := h.Filename()
					log.Printf("Got attachment: %v\n", filename)
				}
			}

		},
	}

	mf := goimap.NewMessageFetcher(&p)

	mf.SetLogger(goimap.NewLogger())

	pollingStarted := make(chan bool)
	pollingEnded := make(chan error)

	go func() {
		mf.Poll(context.Background(), pollingStarted, pollingEnded)
	}()

	<-pollingStarted

	fmt.Println("polling messages...")
	if err := <-pollingEnded; err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

}
