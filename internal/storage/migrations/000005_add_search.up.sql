CREATE VIRTUAL TABLE articles_fts USING fts5(
    title, description, content,
    content='articles', content_rowid='rowid'
);

INSERT INTO articles_fts(rowid, title, description, content)
    SELECT id, title, COALESCE(description, ''), COALESCE(content, '')
    FROM articles;

CREATE TRIGGER articles_ai AFTER INSERT ON articles BEGIN
    INSERT INTO articles_fts(rowid, title, description, content)
        VALUES (new.id, new.title, COALESCE(new.description, ''), COALESCE(new.content, ''));
END;

CREATE TRIGGER articles_ad AFTER DELETE ON articles BEGIN
    INSERT INTO articles_fts(articles_fts, rowid, title, description, content)
        VALUES ('delete', old.id, old.title, COALESCE(old.description, ''), COALESCE(old.content, ''));
END;

CREATE TRIGGER articles_au AFTER UPDATE ON articles BEGIN
    INSERT INTO articles_fts(articles_fts, rowid, title, description, content)
        VALUES ('delete', old.id, old.title, COALESCE(old.description, ''), COALESCE(old.content, ''));
    INSERT INTO articles_fts(rowid, title, description, content)
        VALUES (new.id, new.title, COALESCE(new.description, ''), COALESCE(new.content, ''));
END;
