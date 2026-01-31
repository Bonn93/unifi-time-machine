package share

import (
	"log"
	"time"

	"time-machine/pkg/database"
)

func StartShareLinkCleanupScheduler() {
	log.Println("Starting share link cleanup scheduler...")
	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for {
			<-ticker.C
			log.Println("Running share link cleanup...")
			if err := database.DeleteExpiredShareLinks(); err != nil {
				log.Printf("Error cleaning up expired share links: %v", err)
			}
		}
	}()
}
