-- SQLite does not support DROP COLUMN prior to 3.35.0.
-- Recreate the blogs table without the group_name column.
--
-- foreign_keys is enabled on every connection (see OpenDatabase), and
-- articles.blog_id references blogs(id). Dropping blogs while any table's
-- schema still declares that reference would fail with a foreign key
-- constraint violation. So articles is rebuilt too: first into a plain
-- temp table with no foreign key clause (nothing then references blogs,
-- so dropping it is safe), then recreated with the original foreign key
-- once blogs exists again under its final name.
CREATE TABLE articles_temp (
    id INTEGER PRIMARY KEY,
    blog_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    published_date TIMESTAMP,
    discovered_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_read BOOLEAN DEFAULT FALSE,
    categories TEXT
);

INSERT INTO articles_temp SELECT id, blog_id, title, url, published_date, discovered_date, is_read, categories FROM articles;

CREATE TABLE blogs_backup (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    feed_url TEXT,
    scrape_selector TEXT,
    last_scanned TIMESTAMP
);

INSERT INTO blogs_backup SELECT id, name, url, feed_url, scrape_selector, last_scanned FROM blogs;

DROP TABLE articles;
DROP TABLE blogs;

ALTER TABLE blogs_backup RENAME TO blogs;

CREATE TABLE articles (
    id INTEGER PRIMARY KEY,
    blog_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    published_date TIMESTAMP,
    discovered_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    is_read BOOLEAN DEFAULT FALSE,
    categories TEXT,
    FOREIGN KEY (blog_id) REFERENCES blogs(id)
);

INSERT INTO articles SELECT id, blog_id, title, url, published_date, discovered_date, is_read, categories FROM articles_temp;

DROP TABLE articles_temp;
