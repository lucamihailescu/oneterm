package model

import (
	"encoding/json"
	"time"

	"gorm.io/plugin/soft_delete"
)

type Account struct {
	Id          int    `json:"id" gorm:"column:id;primarykey;autoIncrement"`
	Name        string `json:"name" gorm:"column:name;uniqueIndex:name_del;size:128"`
	AccountType int    `json:"account_type,omitempty" gorm:"column:account_type"`
	Account     string `json:"account" gorm:"column:account"`
	Password    string `json:"password,omitempty" gorm:"column:password"`
	Pk          string `json:"pk,omitempty" gorm:"column:pk"`
	Phrase      string `json:"phrase,omitempty" gorm:"column:phrase"`

	Permissions []string              `json:"permissions,omitempty" gorm:"-"`
	ResourceId  int                   `json:"resource_id,omitempty" gorm:"column:resource_id"`
	CreatorId   int                   `json:"creator_id,omitempty" gorm:"column:creator_id"`
	UpdaterId   int                   `json:"updater_id,omitempty" gorm:"column:updater_id"`
	CreatedAt   time.Time             `json:"created_at,omitempty" gorm:"column:created_at"`
	UpdatedAt   time.Time             `json:"updated_at,omitempty" gorm:"column:updated_at"`
	DeletedAt   soft_delete.DeletedAt `json:"-" gorm:"column:deleted_at;uniqueIndex:name_del"`

	AssetCount int64 `json:"asset_count,omitempty" gorm:"-"`
}

func (m *Account) TableName() string {
	return "account"
}
func (m *Account) SetId(id int) {
	m.Id = id
}
func (m *Account) SetCreatorId(creatorId int) {
	m.CreatorId = creatorId
}
func (m *Account) SetUpdaterId(updaterId int) {
	m.UpdaterId = updaterId
}
func (m *Account) SetResourceId(resourceId int) {
	m.ResourceId = resourceId
}
func (m *Account) GetResourceId() int {
	return m.ResourceId
}
func (m *Account) GetName() string {
	return m.Name
}
func (m *Account) GetId() int {
	return m.Id
}

func (m *Account) SetPerms(perms []string) {
	m.Permissions = perms
}

// MarshalJSON strips credentials from any default JSON serialization of Account.
// Callers that legitimately need to expose Password/Pk/Phrase must use the
// AccountCredentials DTO and go through the MFA-protected reveal endpoint.
// Note: Unmarshal still accepts these fields so create/update payloads work.
func (m Account) MarshalJSON() ([]byte, error) {
	type accountSafe Account
	safe := accountSafe(m)
	safe.Password = ""
	safe.Pk = ""
	safe.Phrase = ""
	return json.Marshal(safe)
}

// AccountCredentials is the only response shape that exposes secrets.
// Returned exclusively by the MFA-gated reveal endpoint.
type AccountCredentials struct {
	Id       int    `json:"id"`
	Name     string `json:"name"`
	Account  string `json:"account"`
	Password string `json:"password"`
	Pk       string `json:"pk"`
	Phrase   string `json:"phrase"`
}

func NewAccountCredentials(a *Account) *AccountCredentials {
	return &AccountCredentials{
		Id:       a.Id,
		Name:     a.Name,
		Account:  a.Account,
		Password: a.Password,
		Pk:       a.Pk,
		Phrase:   a.Phrase,
	}
}

type AccountCount struct {
	Id    int   `json:"id" gorm:"id"`
	Count int64 `json:"count" gorm:"count"`
}
