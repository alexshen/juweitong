package dal

type LikedPost struct {
	MemberId string
	PostId   string
}

type LikedPostsDAO interface {
	Has(record LikedPost) (bool, error)
	Add(record LikedPost) error
}
