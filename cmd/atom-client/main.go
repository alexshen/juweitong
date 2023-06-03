package main

import (
	"flag"
	"log"

	"github.com/alexshen/juweitong/atom"
	"github.com/skratchdot/open-golang/open"
)

var fPost = flag.Int("post", 10, "number of posts to visit")

func main() {
	flag.Parse()
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
		log.Printf("Liked notices: %d", client.LikeNotices(*fPost))
		log.Printf("Liked moments: %d", client.LikeMoments(*fPost))
		log.Printf("Liked ccp notices: %d", client.LikeCCPPosts(*fPost))
		log.Printf("Liked proposals: %d", client.LikeProposals(*fPost))
	}
}
