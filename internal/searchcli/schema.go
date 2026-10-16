package searchcli

// FTS owns its content. Triggers synchronize inserts/deletes in the same transaction.
// Sections are replaced, never updated in place.
const schema = `
CREATE TABLE meta(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE files(path TEXT PRIMARY KEY,hash TEXT NOT NULL,mtime INTEGER NOT NULL,size INTEGER NOT NULL);
CREATE TABLE sections(id INTEGER PRIMARY KEY,path TEXT NOT NULL REFERENCES files(path) ON DELETE CASCADE,heading TEXT NOT NULL,start INTEGER NOT NULL,end INTEGER NOT NULL,body TEXT NOT NULL);
CREATE VIRTUAL TABLE fts USING fts5(heading,body,tokenize='trigram');
CREATE TRIGGER section_insert AFTER INSERT ON sections BEGIN
 INSERT INTO fts(rowid,heading,body) VALUES(new.id,new.heading,new.body);
END;
CREATE TRIGGER section_delete AFTER DELETE ON sections BEGIN
 DELETE FROM fts WHERE rowid=old.id;
END;
PRAGMA user_version=1;
`
