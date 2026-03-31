package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

const userSelectSQL = `SELECT id, username, password_hash, created_at, updated_at
	 FROM users`

// UserRepo implements repository.UserRepository using PostgreSQL.
type UserRepo struct {
	pool *pgxpool.Pool
}

// Compile-time check that UserRepo satisfies UserRepository.
var _ repository.UserRepository = (*UserRepo)(nil)

// NewUserRepo returns a UserRepo backed by the given connection pool.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

// Create inserts a new user and stores a bcrypt password hash.
func (r *UserRepo) Create(ctx context.Context, user *domain.User) error {
	if err := user.ValidateForCreate(); err != nil {
		return fmt.Errorf("postgres: validate user: %w", err)
	}
	username := normalizeUsername(user.Username)

	passwordHash, err := hashPassword(user.Password)
	if err != nil {
		return fmt.Errorf("postgres: hash user password: %w", err)
	}

	row := r.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash)
		 VALUES ($1, $2)
		 RETURNING id, created_at, updated_at`,
		username,
		passwordHash,
	)

	if err := row.Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return fmt.Errorf("postgres: create user: %w", err)
	}

	user.Username = username
	user.PasswordHash = passwordHash
	user.Password = ""

	return nil
}

// GetByUsername retrieves a user by username.
func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	username = normalizeUsername(username)

	row := r.pool.QueryRow(ctx,
		userSelectSQL+`
		 WHERE username = $1`,
		username,
	)

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get user by username %s: %w", username, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get user by username: %w", err)
	}

	return user, nil
}

// GetByID retrieves a user by ID.
func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	row := r.pool.QueryRow(ctx,
		userSelectSQL+`
		 WHERE id = $1`,
		id,
	)

	user, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get user %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get user by id: %w", err)
	}

	return user, nil
}

func scanUser(sc scanner) (*domain.User, error) {
	var user domain.User
	if err := sc.Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &user, nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// normalizeUsername trims surrounding whitespace so create and lookup paths use
// the same canonical username representation.
func normalizeUsername(username string) string {
	return strings.TrimSpace(username)
}
