package db

import (
	"context"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(context.Background(), dsn)
}

func Migrate(pool *pgxpool.Pool, schemaPath string) error {
	b, err := os.ReadFile(schemaPath)
	if err != nil {
		return err
	}
	ctx := context.Background()
	for _, stmt := range SplitSQL(string(b)) {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func GetMeta(pool *pgxpool.Pool, k string) string {
	var v string
	err := pool.QueryRow(context.Background(), `SELECT v FROM meta WHERE k=$1`, k).Scan(&v)
	if err != nil {
		return ""
	}
	return v
}

func SetMeta(pool *pgxpool.Pool, k, v string) error {
	_, err := pool.Exec(context.Background(),
		`INSERT INTO meta(k,v) VALUES($1,$2) ON CONFLICT (k) DO UPDATE SET v=EXCLUDED.v`, k, v)
	return err
}

func Installed(pool *pgxpool.Pool) bool {
	return GetMeta(pool, "installed") == "1"
}

type User struct {
	ID       int64
	LoginID  string
	Email    string
	PassHash *string
	Role     string
	Handle   *string
	Name     string
	Bio      string
	Avatar   *string
	TOTP     *string
	TOTPOn   bool
}

func (u *User) PublicHandle() string {
	if u == nil || u.Handle == nil {
		return ""
	}
	return *u.Handle
}

func UserByLogin(pool *pgxpool.Pool, login string) (*User, error) {
	return scanUser(pool.QueryRow(context.Background(),
		`SELECT id, login_id, email, pass_hash, role, handle, name, bio, avatar, totp_secret, totp_enabled FROM users WHERE login_id=$1`, login))
}

func UserByID(pool *pgxpool.Pool, id int64) (*User, error) {
	return scanUser(pool.QueryRow(context.Background(),
		`SELECT id, login_id, email, pass_hash, role, handle, name, bio, avatar, totp_secret, totp_enabled FROM users WHERE id=$1`, id))
}

func UserByEmail(pool *pgxpool.Pool, email string) (*User, error) {
	return scanUser(pool.QueryRow(context.Background(),
		`SELECT id, login_id, email, pass_hash, role, handle, name, bio, avatar, totp_secret, totp_enabled FROM users WHERE email=$1`, email))
}

func UserByHandle(pool *pgxpool.Pool, h string) (*User, error) {
	return scanUser(pool.QueryRow(context.Background(),
		`SELECT id, login_id, email, pass_hash, role, handle, name, bio, avatar, totp_secret, totp_enabled FROM users WHERE handle=$1`, h))
}

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.LoginID, &u.Email, &u.PassHash, &u.Role, &u.Handle, &u.Name, &u.Bio, &u.Avatar, &u.TOTP, &u.TOTPOn)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func CreateUser(pool *pgxpool.Pool, login, email, role, name string, pass *string, handle *string) (int64, error) {
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users(login_id,email,pass_hash,role,name,handle) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`,
		login, email, pass, role, name, handle).Scan(&id)
	return id, err
}

type Piece struct {
	ID, SiteID, AuthorID                        int64
	Type, Status, Title, Slug, Content, Excerpt string
	SeriesID                                    *int64
	FeatImg, FeatAud, FeatVid                   *string
	FeatPlace                                   string
	AuthorName, AuthorHandle                    string
	SeriesName, SeriesSlug                      string
	Published                                   *string
}

func SplitSQL(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
