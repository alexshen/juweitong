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

type dbSelectedCommunitiesDAO struct {
	db *gorm.DB
}

func NewSelectedCommunitiesDAO(db *gorm.DB) SelectedCommunitiesDAO {
	return &dbSelectedCommunitiesDAO{db}
}

func (o *dbSelectedCommunitiesDAO) FindAll(userId string) ([]string, error) {
	var results []string
	if err := o.db.Model(&SelectedCommunity{}).
		Where("user_id = ?", userId).
		Pluck("name", &results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (o *dbSelectedCommunitiesDAO) Add(record SelectedCommunity) (bool, error) {
	res := o.db.Exec("INSERT OR IGNORE INTO selected_communities (user_id, name) VALUES(?, ?)", record.UserId, record.Name)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected != 0, nil
}

func (o *dbSelectedCommunitiesDAO) Delete(record SelectedCommunity) error {
	return o.db.Delete(record).Error
}
