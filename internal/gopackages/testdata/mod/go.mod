module example.com/mod

go 1.27

require (
	github.com/jmoiron/sqlx v0.0.0
	gorm.io/gorm v0.0.0
)

replace (
	github.com/jmoiron/sqlx => ./fake/sqlx
	gorm.io/gorm => ./fake/gorm
)
