// 読書タスクのDB操作。すべての操作でログイン中のユーザーIDを条件に含める。
package books

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("book not found")

const PageSize = 20

type Store interface {
	List(context.Context, int64, int) ([]Book, bool, error)
	Get(context.Context, int64, int64) (Book, error)
	Create(context.Context, int64, Input) (int64, error)
	Update(context.Context, int64, int64, Input) error
	Delete(context.Context, int64, int64) error
}

type PostgresStore struct{ DB *sql.DB }

func (s PostgresStore) List(ctx context.Context, userID int64, page int) ([]Book, bool, error) {
	// 1件多く取得して次ページの有無を調べる。一覧には大きなメモ本文を読み込まない。
	rows, err := s.DB.QueryContext(ctx, `SELECT id,title,status,created_at,updated_at FROM reading_tasks
 WHERE user_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, userID, PageSize+1, (page-1)*PageSize)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	list := []Book{}
	for rows.Next() {
		var book Book
		if err := rows.Scan(&book.ID, &book.Title, &book.Status, &book.CreatedAt, &book.UpdatedAt); err != nil {
			return nil, false, err
		}
		list = append(list, book)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(list) > PageSize
	if more {
		list = list[:PageSize]
	}
	return list, more, nil
}

func (s PostgresStore) Get(ctx context.Context, userID, id int64) (Book, error) {
	var b Book
	err := s.DB.QueryRowContext(ctx, `SELECT id,title,status,notes,created_at,updated_at FROM reading_tasks
 WHERE user_id=$1 AND id=$2`, userID, id).Scan(&b.ID, &b.Title, &b.Status, &b.Notes, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	return b, err
}

func (s PostgresStore) Create(ctx context.Context, userID int64, input Input) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO reading_tasks (user_id,title,status,notes)
 VALUES ($1,$2,$3,$4) RETURNING id`, userID, input.Title, input.Status, input.Notes).Scan(&id)
	return id, err
}

func (s PostgresStore) Update(ctx context.Context, userID, id int64, input Input) error {
	// 所有者の確認と更新を同じSQLで行う。user_id自体は更新しない。
	result, err := s.DB.ExecContext(ctx, `UPDATE reading_tasks SET title=$3,status=$4,notes=$5,updated_at=now()
 WHERE user_id=$1 AND id=$2`, userID, id, input.Title, input.Status, input.Notes)
	return affected(result, err)
}

func (s PostgresStore) Delete(ctx context.Context, userID, id int64) error {
	result, err := s.DB.ExecContext(ctx, "DELETE FROM reading_tasks WHERE user_id=$1 AND id=$2", userID, id)
	return affected(result, err)
}

func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	// 他人のIDと存在しないIDは同じエラーにし、データの存在を知らせない。
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
