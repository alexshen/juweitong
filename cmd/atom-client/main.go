package main

import (
	"log"

	"github.com/alexshen/juweitong/atom"
	"github.com/samber/lo"
	"github.com/skratchdot/open-golang/open"
)

// rotateCopy rotates the slice and returns a new copy that begins with s[i:]
// followed by s[0:i]
func rotateCopy[S ~[]E, E any](s S, i int) S {
	res := make(S, 0, len(s))
	res = append(res, s[i:]...)
	return append(res, s[0:i]...)
}

func main() {
	client := atom.NewClient()
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
	communities := rotateCopy(client.Communites, client.CurrentCommunityIndex())
	for i, comm := range communities {
		if i != 0 {
			log.Printf("Switching to community: %s", comm.Name)
			j := lo.IndexOf(client.Communites, comm)
			if err := client.SetCurrentCommunity(j); err != nil {
				log.Print(err)
				continue
			}
		}
		log.Printf("Visiting community: %s", comm.Name)
		log.Printf("Liked notices: %d", client.LikeNotices(1))
		log.Printf("Liked moments: %d", client.LikeMoments(1))
		log.Printf("Liked ccp notices: %d", client.LikeCCPPosts(1))
		log.Printf("Liked proposals: %d", client.LikeProposals(1))
	}
}
