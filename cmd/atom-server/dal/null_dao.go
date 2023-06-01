package dal

type NullLikedPostsDAO struct{}

func (o NullLikedPostsDAO) Has(record LikedPost) (bool, error) {
	return false, nil
}

func (o NullLikedPostsDAO) Add(record LikedPost) error {
	return nil
}
