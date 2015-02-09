package main

import (
	"log"
	"time"
)

type MessageRelay struct {
	Renderer SummaryRenderer
	Buffer   *MessageBuffer
	Reloader *Reloader
}

func (r *MessageRelay) Run(received <-chan *ReceivedMessage, done <-chan TerminationRequest, outgoing chan<- OutgoingMessage) {
	tick := time.Tick(1 * time.Second)

	for {
		select {
		case <-tick:
			for _, summary := range r.Buffer.Flush(false) {
				outgoing <- r.Renderer.Render(summary)
			}
		case msg := <-received:
			r.Buffer.Add(msg)
		case req := <-done:
			if req == Reload {
				r.Reloader.RequestReload()
			}
			log.Printf("cleaning up")
			for _, summary := range r.Buffer.Flush(true) {
				outgoing <- r.Renderer.Render(summary)
			}
			close(outgoing)
			break
		}
	}
}
