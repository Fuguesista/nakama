-- CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- DROP DATABASE IF EXISTS nakama CASCADE;
CREATE DATABASE IF NOT EXISTS nakama;
SET DATABASE = nakama;

CREATE TABLE IF NOT EXISTS email_verification_codes (
    email VARCHAR NOT NULL,
    code UUID NOT NULL DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (email, code)
);

CREATE TABLE IF NOT EXISTS users (
    id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR NOT NULL UNIQUE,
    username VARCHAR NOT NULL UNIQUE,
    avatar VARCHAR,
    followers_count INT NOT NULL DEFAULT 0 CHECK (followers_count >= 0),
    followees_count INT NOT NULL DEFAULT 0 CHECK (followees_count >= 0)
);

CREATE TABLE IF NOT EXISTS webauthn_authenticators (
    id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    aaguid BYTES NOT NULL,
    sign_count INT NOT NULL,
    clone_warning BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    webauthn_authenticator_id UUID NOT NULL REFERENCES webauthn_authenticators ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    credential_id VARCHAR NOT NULL,
    public_key BYTES NOT NULL,
    attestation_type VARCHAR NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE INDEX unique_webauthn_credentials (user_id, credential_id)
);

CREATE TABLE IF NOT EXISTS follows (
    follower_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    followee_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    PRIMARY KEY (follower_id, followee_id)
);

CREATE TABLE IF NOT EXISTS posts (
    id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    content VARCHAR NOT NULL,
    spoiler_of VARCHAR,
    nsfw BOOLEAN NOT NULL DEFAULT false,
    comments_count INT NOT NULL DEFAULT 0 CHECK (comments_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    INDEX sorted_posts (created_at DESC, id)
);

ALTER TABLE posts DROP COLUMN IF EXISTS likes_count;

CREATE TABLE IF NOT EXISTS post_reactions (
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES posts ON DELETE CASCADE,
    reaction VARCHAR NOT NULL,
    type VARCHAR NOT NULL CHECK (type = 'emoji' OR type = 'custom'),
    PRIMARY KEY (user_id, post_id, reaction)
);

CREATE TABLE IF NOT EXISTS post_subscriptions (
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES posts ON DELETE CASCADE,
    PRIMARY KEY (user_id, post_id)
);

CREATE TABLE IF NOT EXISTS timeline (
    id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES posts ON DELETE CASCADE,
    UNIQUE INDEX unique_timeline_items (user_id, post_id)
);

CREATE TABLE IF NOT EXISTS comments (
    id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES posts ON DELETE CASCADE,
    content VARCHAR NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    INDEX sorted_comments (created_at DESC, id)
);

ALTER TABLE comments DROP COLUMN IF EXISTS likes_count;

CREATE TABLE IF NOT EXISTS comment_reactions (
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    comment_id UUID NOT NULL REFERENCES comments ON DELETE CASCADE,
    reaction VARCHAR NOT NULL,
    type VARCHAR NOT NULL CHECK (type = 'emoji' OR type = 'custom'),
    PRIMARY KEY (user_id, comment_id, reaction)
);

CREATE TABLE IF NOT EXISTS notifications (
    id UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users ON DELETE CASCADE,
    actors VARCHAR[] NOT NULL,
    type VARCHAR NOT NULL,
    post_id UUID REFERENCES posts ON DELETE CASCADE,
    read_at TIMESTAMPTZ,
    issued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    INDEX sorted_notifications (issued_at DESC, id),
    UNIQUE INDEX unique_notifications (user_id, type, post_id, read_at)
);

-- INSERT INTO users (id, email, username) VALUES
--     ('24ca6ce6-b3e9-4276-a99a-45c77115cc9f', 'shinji@example.org', 'shinji'),
--     ('93dfcef9-0b45-46ae-933c-ea52fbf80edb', 'rei@example.org', 'rei');

-- INSERT INTO posts (id, user_id, content, comments_count) VALUES
--     ('c592451b-fdd2-430d-8d49-e75f058c3dce', '24ca6ce6-b3e9-4276-a99a-45c77115cc9f', 'sample post', 1);
-- INSERT INTO post_subscriptions (user_id, post_id) VALUES
--     ('24ca6ce6-b3e9-4276-a99a-45c77115cc9f', 'c592451b-fdd2-430d-8d49-e75f058c3dce');
-- INSERT INTO timeline (id, user_id, post_id) VALUES
--     ('d7490258-1f2f-4a75-8fbb-1846ccde9543', '24ca6ce6-b3e9-4276-a99a-45c77115cc9f', 'c592451b-fdd2-430d-8d49-e75f058c3dce');

-- INSERT INTO comments (id, user_id, post_id, content) VALUES
--     ('648e60bf-b0ab-42e6-8e48-10f797b19c49', '24ca6ce6-b3e9-4276-a99a-45c77115cc9f', 'c592451b-fdd2-430d-8d49-e75f058c3dce', 'sample comment');
