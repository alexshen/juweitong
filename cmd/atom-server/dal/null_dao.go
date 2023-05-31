package dal

import "github.com/google/uuid"

type NullClientsDAO struct{}

func (o NullClientsDAO) Has(id string) (bool, error) {
	return true, nil
}

func (o NullClientsDAO) Create() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

type NullLikedPostsDAO struct{}

func (o NullLikedPostsDAO) Has(record LikedPost) (bool, error) {
	return false, nil
}

func (o NullLikedPostsDAO) Add(record LikedPost) error {
	return nil
}
