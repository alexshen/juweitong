package dal

import (
	"gorm.io/gorm"
)

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

type dbLikedPostsDAO struct {
	db *gorm.DB
}

func NewDBLikedPostsDAO(db *gorm.DB) LikedPostsDAO {
	return &dbLikedPostsDAO{db}
}

func (o *dbLikedPostsDAO) Has(record LikedPost) (bool, error) {
	return exists(o.db, "SELECT 1 FROM liked_posts WHERE member_id = ? AND post_id = ?",
		record.MemberId, record.PostId)
}

func (o *dbLikedPostsDAO) Add(record LikedPost) error {
	return o.db.Create(&record).Error
}
