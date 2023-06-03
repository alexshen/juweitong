package main

import (
	"log"

	"github.com/alexshen/juweitong/atom"
	"github.com/skratchdot/open-golang/open"
)

func main() {
	client := atom.NewClient(atom.NullLikedPostsHistory{})
	loggedIn := make(chan struct{})
	url, err := client.StartQRLogin(func() {
		log.Print("Logged in")
		close(loggedIn)
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("QR Code: %s\n", url)
	open.Run(url)
	<-loggedIn
	for _, comm := range client.Communities() {
		log.Printf("Switching to community: %s, %s", comm.Name, comm.MemberId)
		if err := client.SetCurrentCommunityById(comm.MemberId); err != nil {
			log.Print(err)
			continue
		}
		log.Printf("Visiting community: %s", comm.Name)
		log.Printf("Liked notices: %d", client.LikeNotices(1))
		log.Printf("Liked moments: %d", client.LikeMoments(1))
		log.Printf("Liked ccp notices: %d", client.LikeCCPPosts(1))
		log.Printf("Liked proposals: %d", client.LikeProposals(1))
	}
}
