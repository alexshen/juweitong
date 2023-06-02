package dal

type LikedPost struct {
	MemberId string `gorm:"primaryKey"`
	PostId   string `gorm:"primaryKey"`
}

type LikedPostsDAO interface {
	Has(record LikedPost) (bool, error)
	Add(record LikedPost) error
}

type SelectedCommunity struct {
	UserId string `gorm:"primaryKey"`
	Name   string `gorm:"primaryKey"`
}

type SelectedCommunitiesDAO interface {
	// FindAll returns all the selected member ids
	FindAll(userId string) ([]string, error)
	// Add returns true if the record is inserted
	Add(record SelectedCommunity) (bool, error)
	Delete(record SelectedCommunity) error
}
