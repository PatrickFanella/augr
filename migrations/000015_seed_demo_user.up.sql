-- Seed a demo user for paper trading authentication.
-- Credentials: username=demo, password=demo-pass
INSERT INTO users (username, password_hash)
VALUES ('demo', '$2a$10$2KP4leMWDh6y1agoINSjQeslRLJctCrunNJrBihnOSscgfhZgappu')
ON CONFLICT (username) DO NOTHING;
