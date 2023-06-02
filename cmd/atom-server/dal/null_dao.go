package dal

type NullLikedPostsDAO struct{}

func (o NullLikedPostsDAO) Has(record LikedPost) (bool, error) {
	return false, nil
}

func (o NullLikedPostsDAO) Add(record LikedPost) error {
	return nil
}

type NullSelectedCommunitiesDAO struct{}

func (o NullSelectedCommunitiesDAO) FindAll(userId string) ([]string, error) {
	return nil, nil
}

func (o NullSelectedCommunitiesDAO) Add(s SelectedCommunity) (bool, error) {
	return true, nil
}

func (o NullSelectedCommunitiesDAO) Delete(s SelectedCommunity) error {
	return nil
}
