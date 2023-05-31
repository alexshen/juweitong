package dal

type ClientsDAO interface {
	Has(id string) (bool, error)
	Create() (string, error)
}

type LikedPost struct {
	ClientId    string
	CommunityId string
	PostId      string
}

type LikedPostsDAO interface {
	Has(record LikedPost) (bool, error)
	Add(record LikedPost) error
}
