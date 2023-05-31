package dal

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type dbClientsDAO struct {
	db *gorm.DB
}

type client struct {
	ID string
}

func NewDBClientsDAO(db *gorm.DB) ClientsDAO {
	return &dbClientsDAO{db}
}

func exists(db *gorm.DB, statement string, args ...any) (bool, error) {
	var result int
	err := db.Raw(statement, args...).Scan(&result).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (o *dbClientsDAO) Has(id string) (bool, error) {
	return exists(o.db, "SELECT 1 FROM clients WHERE id = ?", id)
}

func (o *dbClientsDAO) Create() (string, error) {
	for {
		id, err := uuid.NewRandom()
		if err != nil {
			return "", err
		}
		c := client{id.String()}
		result := o.db.Create(&c)
		if result.Error != gorm.ErrDuplicatedKey {
			return c.ID, nil
		}
	}
}

type dbLikedPostsDAO struct {
	db *gorm.DB
}

func NewDBLikedPostsDAO(db *gorm.DB) LikedPostsDAO {
	return &dbLikedPostsDAO{db}
}

func (o *dbLikedPostsDAO) Has(record LikedPost) (bool, error) {
	return exists(o.db, "SELECT 1 FROM liked_posts WHERE client_id = ? AND community_id = ? AND post_id = ?",
		record.ClientId, record.CommunityId, record.PostId)
}

func (o *dbLikedPostsDAO) Add(record LikedPost) error {
	return o.db.Create(&record).Error
}
