package nakama

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/cockroach-go/v2/crdb"
	"github.com/lib/pq"
)

const (
	postContentMaxLength = 2048
	postSpoilerMaxLength = 64
)

var (
	// ErrInvalidPostID denotes an invalid post ID; that is not uuid.
	ErrInvalidPostID = InvalidArgumentError("invalid post ID")
	// ErrInvalidContent denotes an invalid content.
	ErrInvalidContent = InvalidArgumentError("invalid content")
	// ErrInvalidSpoiler denotes an invalid spoiler title.
	ErrInvalidSpoiler = InvalidArgumentError("invalid spoiler")
	// ErrPostNotFound denotes a not found post.
	ErrPostNotFound = NotFoundError("post not found")
	// ErrInvalidUpdatePostParams denotes invalid params to update a post, that is no params altogether.
	ErrInvalidUpdatePostParams = InvalidArgumentError("invalid update post params")
	// ErrInvalidCursor denotes an invalid cursor, that is not base64 encoded and has a key and timestamp separated by comma.
	ErrInvalidCursor = InvalidArgumentError("invalid cursor")
	// ErrInvalidReaction denotes an invalid reaction, that may by an invalid reaction type, or invalid reaction by itslef,
	// not a valid emoji, or invalid reaction image URL.
	ErrInvalidReaction  = InvalidArgumentError("invalid reaction")
	ErrUpdatePostDenied = PermissionDeniedError("update post denied")
)

// Post model.
type Post struct {
	ID            string     `json:"id"`
	UserID        string     `json:"-"`
	Content       string     `json:"content"`
	SpoilerOf     *string    `json:"spoilerOf"`
	NSFW          bool       `json:"nsfw"`
	Reactions     []Reaction `json:"reactions"`
	CommentsCount int        `json:"commentsCount"`
	MediaURLs     []string   `json:"mediaURLs"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	User          *User      `json:"user,omitempty"`
	Mine          bool       `json:"mine"`
	Subscribed    bool       `json:"subscribed"`
}

type Reaction struct {
	Type     string `json:"type"`
	Reaction string `json:"reaction"`
	Count    uint64 `json:"count"`
	Reacted  *bool  `json:"reacted,omitempty"`
}

type userReaction struct {
	Reaction string `json:"reaction"`
	Type     string `json:"type"`
}

// ToggleSubscriptionOutput response.
type ToggleSubscriptionOutput struct {
	Subscribed bool `json:"subscribed"`
}

type Posts []Post

func (pp Posts) EndCursor() *string {
	if len(pp) == 0 {
		return nil
	}

	last := pp[len(pp)-1]
	return ptrString(encodeCursor(last.ID, last.CreatedAt))
}

type PostsOpts struct {
	Username *string
	Tag      *string
}

type PostsOpt func(*PostsOpts)

func PostsFromUser(username string) PostsOpt {
	return func(opts *PostsOpts) {
		opts.Username = &username
	}
}

func PostsTagged(tag string) PostsOpt {
	return func(opts *PostsOpts) {
		opts.Tag = &tag
	}
}

// Posts in descending order and with backward pagination.
// They can be filtered from a specific user by using `PostsFromUser` option
// in this late case, user field won't be populated.
// They can also be filtered by tag using `PostsTagged`.
func (s *Service) Posts(ctx context.Context, last uint64, before *string, opts ...PostsOpt) (Posts, error) {
	var options PostsOpts
	for _, o := range opts {
		o(&options)
	}

	if options.Username != nil {
		*options.Username = strings.TrimSpace(*options.Username)
		if !ValidUsername(*options.Username) {
			return nil, ErrInvalidUsername
		}
	}

	var beforePostID string
	var beforeCreatedAt time.Time

	if before != nil {
		var err error
		beforePostID, beforeCreatedAt, err = decodeCursor(*before)
		if err != nil || !reUUID.MatchString(beforePostID) {
			return nil, ErrInvalidCursor
		}
	}

	uid, auth := ctx.Value(KeyAuthUserID).(string)
	last = normalizePageSize(last)
	query, args, err := buildQuery(`
		SELECT posts.id
		, posts.content
		, posts.spoiler_of
		, posts.nsfw
		, posts.reactions
		, posts.comments_count
		, posts.media
		, posts.created_at
		, posts.updated_at
		{{ if .auth }}
		, posts.user_id = @uid AS post_mine
		, reactions.user_reactions
		, subscriptions.user_id IS NOT NULL AS post_subscribed
		{{ end }}
		{{ if not .username }}
		, users.username
		, users.avatar
		{{ end }}
		FROM posts
		{{ if .auth }}
		LEFT JOIN (
			SELECT user_id
			, post_id
			, json_agg(json_build_object('reaction', reaction, 'type', type)) AS user_reactions
			FROM post_reactions
			GROUP BY user_id, post_id
		) AS reactions ON reactions.user_id = @uid AND reactions.post_id = posts.id
		LEFT JOIN post_subscriptions AS subscriptions
			ON subscriptions.user_id = @uid AND subscriptions.post_id = posts.id
		{{ end }}
		{{ if not .username }}
		INNER JOIN users ON posts.user_id = users.id
		{{ end }}
		{{ if .tag }}
		INNER JOIN post_tags ON post_tags.post_id = posts.id AND post_tags.tag = @tag
		{{ end }}
		{{ if or .username (and .beforePostID .beforeCreatedAt) }}
		WHERE
		{{ end }}
		{{ if .username }}
			posts.user_id = (SELECT id FROM users WHERE username = @username)
		{{ end }}
		{{ if and .beforePostID .beforeCreatedAt }}
			{{ if .username }}
			AND
			{{ end }}
			posts.created_at <= @beforeCreatedAt
			AND (
				posts.id < @beforePostID
					OR posts.created_at < @beforeCreatedAt
			)
		{{ end }}
		ORDER BY posts.created_at DESC, posts.id ASC
		LIMIT @last`, map[string]interface{}{
		"auth":            auth,
		"uid":             uid,
		"username":        options.Username,
		"tag":             options.Tag,
		"last":            last,
		"beforePostID":    beforePostID,
		"beforeCreatedAt": beforeCreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("could not build posts sql query: %w", err)
	}

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("could not query select posts: %w", err)
	}

	defer rows.Close()

	var pp Posts
	for rows.Next() {
		var p Post
		var u User
		var avatar sql.NullString
		var rawReactions []byte
		var rawUserReactions []byte
		var media []string
		dest := []interface{}{
			&p.ID,
			&p.Content,
			&p.SpoilerOf,
			&p.NSFW,
			&rawReactions,
			&p.CommentsCount,
			pq.Array(&media),
			&p.CreatedAt,
			&p.UpdatedAt,
		}
		if auth {
			dest = append(dest, &p.Mine, &rawUserReactions, &p.Subscribed)
		}
		if options.Username == nil {
			dest = append(dest, &u.Username, &avatar)
		}

		if err = rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("could not scan post: %w", err)
		}

		if rawReactions != nil {
			err = json.Unmarshal(rawReactions, &p.Reactions)
			if err != nil {
				return nil, fmt.Errorf("could not json unmarshall post reactions: %w", err)
			}
		}

		if rawUserReactions != nil {
			var userReactions []userReaction
			err = json.Unmarshal(rawUserReactions, &userReactions)
			if err != nil {
				return nil, fmt.Errorf("could not json unmarshall user post reactions: %w", err)
			}

			for i, r := range p.Reactions {
				var reacted bool
				for _, ur := range userReactions {
					if r.Type == ur.Type && r.Reaction == ur.Reaction {
						reacted = true
						break
					}
				}
				p.Reactions[i].Reacted = &reacted
			}
		}

		if options.Username == nil {
			u.AvatarURL = s.avatarURL(avatar)
			p.User = &u
		}

		p.MediaURLs = s.mediaURLs(media)
		pp = append(pp, p)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("could not iterate posts rows: %w", err)
	}

	return pp, nil
}

// PostStream to receive posts in realtime.
func (s *Service) PostStream(ctx context.Context) (<-chan Post, error) {
	pp := make(chan Post)
	unsub, err := s.PubSub.Sub(postsTopic, func(data []byte) {
		go func(r io.Reader) {
			var p Post
			err := gob.NewDecoder(r).Decode(&p)
			if err != nil {
				_ = s.Logger.Log("error", fmt.Errorf("could not gob decode post: %w", err))
				return
			}

			pp <- p
		}(bytes.NewReader(data))
	})
	if err != nil {
		return nil, fmt.Errorf("could not subscribe to posts: %w", err)
	}

	go func() {
		<-ctx.Done()
		if err := unsub(); err != nil {
			_ = s.Logger.Log("error", fmt.Errorf("could not unsubcribe from posts: %w", err))
			// don't return
		}

		close(pp)
	}()

	return pp, nil
}

// Post with the given ID.
func (s *Service) Post(ctx context.Context, postID string) (Post, error) {
	var p Post
	if !reUUID.MatchString(postID) {
		return p, ErrInvalidPostID
	}

	uid, auth := ctx.Value(KeyAuthUserID).(string)
	query, args, err := buildQuery(`
		SELECT posts.id
			, posts.content
			, posts.spoiler_of
			, posts.nsfw
			, posts.reactions
			, posts.comments_count
			, posts.media
			, posts.created_at
			, posts.updated_at
			, users.username
			, users.avatar
			{{if .auth}}
			, posts.user_id = @uid AS mine
			, reactions.user_reactions
			, subscriptions.user_id IS NOT NULL AS subscribed
		{{end}}
		FROM posts
		INNER JOIN users ON posts.user_id = users.id
		{{if .auth}}
		LEFT JOIN (
			SELECT user_id
			, post_id
			, json_agg(json_build_object('reaction', reaction, 'type', type)) AS user_reactions
			FROM post_reactions
			GROUP BY user_id, post_id
		) AS reactions ON reactions.user_id = @uid AND reactions.post_id = posts.id
		LEFT JOIN post_subscriptions AS subscriptions
			ON subscriptions.user_id = @uid AND subscriptions.post_id = posts.id
		{{end}}
		WHERE posts.id = @post_id`, map[string]interface{}{
		"auth":    auth,
		"uid":     uid,
		"post_id": postID,
	})
	if err != nil {
		return p, fmt.Errorf("could not build post sql query: %w", err)
	}

	var rawReactions []byte
	var rawUserReactions []byte
	var u User
	var avatar sql.NullString
	var media []string
	dest := []interface{}{
		&p.ID,
		&p.Content,
		&p.SpoilerOf,
		&p.NSFW,
		&rawReactions,
		&p.CommentsCount,
		pq.Array(&media),
		&p.CreatedAt,
		&p.UpdatedAt,
		&u.Username,
		&avatar,
	}
	if auth {
		dest = append(dest, &p.Mine, &rawUserReactions, &p.Subscribed)
	}
	err = s.DB.QueryRowContext(ctx, query, args...).Scan(dest...)
	if err == sql.ErrNoRows {
		return p, ErrPostNotFound
	}

	if err != nil {
		return p, fmt.Errorf("could not query select post: %w", err)
	}

	if rawReactions != nil {
		err = json.Unmarshal(rawReactions, &p.Reactions)
		if err != nil {
			return p, fmt.Errorf("could not json unmarshall post reactions: %w", err)
		}
	}

	if rawUserReactions != nil {
		var userReactions []userReaction
		err = json.Unmarshal(rawUserReactions, &userReactions)
		if err != nil {
			return p, fmt.Errorf("could not json unmarshall user post reactions: %w", err)
		}

		for i, r := range p.Reactions {
			var reacted bool
			for _, ur := range userReactions {
				if r.Type == ur.Type && r.Reaction == ur.Reaction {
					reacted = true
					break
				}
			}
			p.Reactions[i].Reacted = &reacted
		}
	}

	p.MediaURLs = s.mediaURLs(media)
	u.AvatarURL = s.avatarURL(avatar)
	p.User = &u

	return p, nil
}

type UpdatePost struct {
	Content   *string `json:"content"`
	SpoilerOf *string `json:"spoilerOf"`
	NSFW      *bool   `json:"nsfw"`
}

func (params UpdatePost) Empty() bool {
	return params.Content == nil && params.NSFW == nil && params.SpoilerOf == nil
}

type UpdatedPost struct {
	Content   string    `json:"content"`
	SpoilerOf *string   `json:"spoilerOf"`
	NSFW      bool      `json:"nsfw"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Service) UpdatePost(ctx context.Context, postID string, params UpdatePost) (UpdatedPost, error) {
	var out UpdatedPost

	createdAt, err := s.postCreatedAt(ctx, postID)
	if err != nil {
		return out, err
	}

	isRecent := time.Since(createdAt) < time.Minute*15
	if !isRecent {
		return out, ErrUpdatePostDenied
	}

	return s.updatePost(ctx, postID, params)
}

func (s *Service) postCreatedAt(ctx context.Context, postID string) (time.Time, error) {
	const q = `
		SELECT created_at FROM posts WHERE id = $1
	`

	var createdAt time.Time
	err := s.DB.QueryRowContext(ctx, q, postID).Scan(&createdAt)
	if err == sql.ErrNoRows {
		return createdAt, ErrPostNotFound
	}

	if err != nil {
		return createdAt, fmt.Errorf("sql query select post created at: %w", err)
	}

	return createdAt, nil
}

func (s *Service) updatePost(ctx context.Context, postID string, params UpdatePost) (UpdatedPost, error) {
	var updated UpdatedPost
	if params.Empty() {
		return updated, ErrInvalidUpdatePostParams
	}

	uid, ok := ctx.Value(KeyAuthUserID).(string)
	if !ok {
		return updated, ErrUnauthenticated
	}

	if !reUUID.MatchString(postID) {
		return updated, ErrInvalidPostID
	}

	if params.Content != nil {
		*params.Content = smartTrim(*params.Content)
		if *params.Content == "" || utf8.RuneCountInString(*params.Content) > postContentMaxLength {
			return updated, ErrInvalidContent
		}
	}

	if params.SpoilerOf != nil {
		*params.SpoilerOf = smartTrim(*params.SpoilerOf)
		if *params.SpoilerOf == "" || utf8.RuneCountInString(*params.SpoilerOf) > postSpoilerMaxLength {
			return updated, ErrInvalidSpoiler
		}
	}

	var set []string
	if params.Content != nil {
		set = append(set, "content = @content")
	}
	if params.SpoilerOf != nil {
		set = append(set, "spoiler_of = @spoiler_of")
	}
	if params.NSFW != nil {
		set = append(set, "nsfw = @nsfw")
	}

	set = append(set, "updated_at = now()")

	query, args, err := buildQuery(`
		UPDATE posts
		SET {{ .set }}
		WHERE id = @post_id
			AND user_id = @auth_user_id
		RETURNING content, spoiler_of, nsfw, updated_at
		`, map[string]interface{}{
		"content":      params.Content,
		"spoiler_of":   params.SpoilerOf,
		"nsfw":         params.NSFW,
		"set":          strings.Join(set, ", "),
		"post_id":      postID,
		"auth_user_id": uid,
	})
	if err != nil {
		return updated, fmt.Errorf("could not sql update post: %w", err)
	}

	row := s.DB.QueryRowContext(ctx, query, args...)
	err = row.Scan(&updated.Content, &updated.SpoilerOf, &updated.NSFW, &updated.UpdatedAt)
	if err != nil {
		return updated, fmt.Errorf("could not sql update post content: %w", err)
	}

	return updated, nil
}

func (s *Service) DeletePost(ctx context.Context, postID string) error {
	uid, ok := ctx.Value(KeyAuthUserID).(string)
	if !ok {
		return ErrUnauthenticated
	}

	if !reUUID.MatchString(postID) {
		return ErrInvalidPostID
	}

	query := "DELETE FROM posts WHERE id = $1 AND user_id = $2"
	_, err := s.DB.ExecContext(ctx, query, postID, uid)
	if err != nil {
		return fmt.Errorf("could not sql delete post: %w", err)
	}

	return nil
}

type ReactionInput struct {
	Type     string `json:"type"`
	Reaction string `json:"reaction"`
}

func (s *Service) TogglePostReaction(ctx context.Context, postID string, in ReactionInput) ([]Reaction, error) {
	uid, ok := ctx.Value(KeyAuthUserID).(string)
	if !ok {
		return nil, ErrUnauthenticated
	}

	if !reUUID.MatchString(postID) {
		return nil, ErrInvalidPostID
	}

	if in.Type != "emoji" || in.Reaction == "" {
		return nil, ErrInvalidReaction
	}

	if in.Type == "emoji" && !validEmoji(in.Reaction) {
		return nil, ErrInvalidReaction
	}

	var out []Reaction
	err := crdb.ExecuteTx(ctx, s.DB, nil, func(tx *sql.Tx) error {
		out = nil

		var rawReactions []byte
		var rawUserReactions []byte
		query := `
			SELECT posts.reactions, reactions.user_reactions
			FROM posts
			LEFT JOIN (
				SELECT user_id
				, post_id
				, json_agg(json_build_object('reaction', reaction, 'type', type)) AS user_reactions
				FROM post_reactions
				GROUP BY user_id, post_id
			) AS reactions ON reactions.user_id = $1 AND reactions.post_id = posts.id
			WHERE posts.id = $2`
		row := tx.QueryRowContext(ctx, query, uid, postID)
		err := row.Scan(&rawReactions, &rawUserReactions)
		if err == sql.ErrNoRows {
			return ErrPostNotFound
		}

		if err != nil {
			return fmt.Errorf("could not sql scan post and user reactions: %w", err)
		}

		var reactions []Reaction
		if rawReactions != nil {
			err = json.Unmarshal(rawReactions, &reactions)
			if err != nil {
				return fmt.Errorf("could not json unmarshall post reactions: %w", err)
			}
		}

		var userReactions []userReaction
		if rawUserReactions != nil {
			err = json.Unmarshal(rawUserReactions, &userReactions)
			if err != nil {
				return fmt.Errorf("could not json unmarshall user post reactions: %w", err)
			}
		}

		userReactionIdx := -1
		for i, ur := range userReactions {
			if ur.Type == in.Type && ur.Reaction == in.Reaction {
				userReactionIdx = i
				break
			}
		}

		reacted := userReactionIdx != -1
		if !reacted {
			query = "INSERT INTO post_reactions (user_id, post_id, type, reaction) VALUES ($1, $2, $3, $4)"
			_, err = tx.ExecContext(ctx, query, uid, postID, in.Type, in.Reaction)
			if err != nil {
				return fmt.Errorf("could not sql insert post reaction: %w", err)
			}
		} else {
			query = `
				DELETE FROM post_reactions
				WHERE user_id = $1
					AND post_id = $2
					AND type = $3
					AND reaction = $4
			`
			_, err = tx.ExecContext(ctx, query, uid, postID, in.Type, in.Reaction)
			if err != nil {
				return fmt.Errorf("could not sql delete post reaction: %w", err)
			}
		}

		if reacted {
			userReactions = append(userReactions[:userReactionIdx], userReactions[userReactionIdx+1:]...)
		} else {
			userReactions = append(userReactions, userReaction{
				Type:     in.Type,
				Reaction: in.Reaction,
			})
		}

		var updated bool
		zeroReactionsIdx := -1
		for i, r := range reactions {
			if !(r.Type == in.Type && r.Reaction == in.Reaction) {
				continue
			}

			if !reacted {
				reactions[i].Count++
			} else {
				reactions[i].Count--
				if reactions[i].Count == 0 {
					zeroReactionsIdx = i
				}
			}
			updated = true
			break
		}

		if !updated {
			reactions = append(reactions, Reaction{
				Type:     in.Type,
				Reaction: in.Reaction,
				Count:    1,
			})
		}

		if zeroReactionsIdx != -1 {
			reactions = append(reactions[:zeroReactionsIdx], reactions[zeroReactionsIdx+1:]...)
		}

		rawReactions, err = json.Marshal(reactions)
		if err != nil {
			return fmt.Errorf("could not json marshall post reactions: %w", err)
		}

		query = "UPDATE posts SET reactions = $1 WHERE posts.id = $2"
		_, err = tx.ExecContext(ctx, query, rawReactions, postID)
		if err != nil {
			return fmt.Errorf("could not sql update post reactions: %w", err)
		}

		if len(userReactions) != 0 {
			for i, r := range reactions {
				var reacted bool
				for _, ur := range userReactions {
					if r.Type == ur.Type && r.Reaction == ur.Reaction {
						reacted = true
						break
					}
				}
				reactions[i].Reacted = &reacted
			}
		}

		out = reactions

		return nil
	})
	if err != nil {
		return nil, err
	}

	return out, nil
}

// TogglePostSubscription so you can stop receiving notifications from a thread.
func (s *Service) TogglePostSubscription(ctx context.Context, postID string) (ToggleSubscriptionOutput, error) {
	var out ToggleSubscriptionOutput
	uid, ok := ctx.Value(KeyAuthUserID).(string)
	if !ok {
		return out, ErrUnauthenticated
	}

	if !reUUID.MatchString(postID) {
		return out, ErrInvalidPostID
	}

	err := crdb.ExecuteTx(ctx, s.DB, nil, func(tx *sql.Tx) error {
		query := `SELECT EXISTS (
			SELECT 1 FROM post_subscriptions WHERE user_id = $1 AND post_id = $2
		)`
		err := tx.QueryRowContext(ctx, query, uid, postID).Scan(&out.Subscribed)
		if err != nil {
			return fmt.Errorf("could not query select post subscription existence: %w", err)
		}

		if out.Subscribed {
			query = "DELETE FROM post_subscriptions WHERE user_id = $1 AND post_id = $2"
			if _, err = tx.ExecContext(ctx, query, uid, postID); err != nil {
				return fmt.Errorf("could not delete post subscription: %w", err)
			}
		} else {
			query = "INSERT INTO post_subscriptions (user_id, post_id) VALUES ($1, $2)"
			_, err = tx.ExecContext(ctx, query, uid, postID)
			if isForeignKeyViolation(err) {
				return ErrPostNotFound
			}

			if err != nil {
				return fmt.Errorf("could not insert post subscription: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return out, err
	}

	out.Subscribed = !out.Subscribed

	return out, nil
}

const postsTopic = "posts"

func (s *Service) broadcastPost(p Post) {
	var b bytes.Buffer
	err := gob.NewEncoder(&b).Encode(p)
	if err != nil {
		_ = s.Logger.Log("error", fmt.Errorf("could not gob encode post: %w", err))
		return
	}

	err = s.PubSub.Pub(postsTopic, b.Bytes())
	if err != nil {
		_ = s.Logger.Log("error", fmt.Errorf("could not publish post: %w", err))
		return
	}
}
