// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package agents

import (
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jmoiron/sqlx"
	"github.com/mattermost/mattermost-plugin-ai/mmapi"
)

type builder interface {
	ToSql() (string, []interface{}, error)
}

func (p *AgentsService) SetupDB() error {
	client := mmapi.NewDBClient(p.pluginAPI)
	p.db = client.DB
	p.builder = client.Builder()

	return p.SetupTables()
}

func (p *AgentsService) doQuery(dest interface{}, b builder) error {
	sqlString, args, err := b.ToSql()
	if err != nil {
		return fmt.Errorf("failed to build sql: %w", err)
	}

	sqlString = p.db.Rebind(sqlString)

	return sqlx.Select(p.db, dest, sqlString, args...)
}

func (p *AgentsService) execBuilder(b builder) (sql.Result, error) {
	sqlString, args, err := b.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build sql: %w", err)
	}

	sqlString = p.db.Rebind(sqlString)

	return p.db.Exec(sqlString, args...)
}

func (p *AgentsService) SetupTables() error {
	if _, err := p.db.Exec(`
		CREATE TABLE IF NOT EXISTS LLM_PostMeta (
			RootPostID TEXT NOT NULL REFERENCES Posts(ID) ON DELETE CASCADE PRIMARY KEY,
			Title TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("can't create llm titles table: %w", err)
	}

	// This fixes data retention issues when a post is deleted for an older version of the postmeta table.
	// Migrate from the old table using `"INSERT INTO LLM_PostMeta(RootPostID, Title) SELECT RootPostID, Title from LLM_Threads"`
	if _, err := p.db.Exec(`ALTER TABLE IF EXISTS LLM_Threads DROP CONSTRAINT IF EXISTS llm_threads_rootpostid_fkey;`); err != nil {
		return fmt.Errorf("failed to migrate constraint: %w", err)
	}

	return nil
}

func (p *AgentsService) saveTitleAsync(threadID, title string) {
	go func() {
		if err := p.saveTitle(threadID, title); err != nil {
			p.pluginAPI.Log.Error("failed to save title: " + err.Error())
		}
	}()
}

func (p *AgentsService) saveTitle(threadID, title string) error {
	_, err := p.execBuilder(p.builder.Insert("LLM_PostMeta").
		Columns("RootPostID", "Title").
		Values(threadID, title).
		Suffix("ON CONFLICT (RootPostID) DO UPDATE SET Title = ?", title))
	return err
}

// This is a different AIThread struct than the one in conversations.go, used for database queries
type aiThreadData struct {
	ID         string
	Message    string
	ChannelID  string
	Title      string
	ReplyCount int
	UpdateAt   int64
}

func (p *AgentsService) getAIThreads(dmChannelIDs []string) ([]AIThread, error) {
	var dbPosts []aiThreadData
	if err := p.doQuery(&dbPosts, p.builder.
		Select(
			"p.Id",
			"p.Message",
			"p.ChannelID",
			"COALESCE(t.Title, '') as Title",
			"(SELECT COUNT(*) FROM Posts WHERE Posts.RootId = p.Id AND DeleteAt = 0) AS ReplyCount",
			"p.UpdateAt",
		).
		From("Posts as p").
		Where(sq.Eq{"ChannelID": dmChannelIDs}).
		Where(sq.Eq{"RootId": ""}).
		Where(sq.Eq{"DeleteAt": 0}).
		LeftJoin("LLM_PostMeta as t ON t.RootPostID = p.Id").
		OrderBy("CreateAt DESC").
		Limit(60).
		Offset(0),
	); err != nil {
		return nil, fmt.Errorf("failed to get posts for bot DM: %w", err)
	}

	// Convert from internal type to public AIThread type
	result := make([]AIThread, len(dbPosts))
	for i, post := range dbPosts {
		result[i] = AIThread{
			ID:        post.ID,
			Title:     post.Title,
			ChannelID: post.ChannelID,
			BotID:     "", // We don't have this info in the query
			UpdatedAt: post.UpdateAt,
		}
	}

	return result, nil
}
