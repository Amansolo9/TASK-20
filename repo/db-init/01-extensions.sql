-- Runs once when the Postgres container is first created.
-- GORM AutoMigrate handles all tables, but we pre-create the pgcrypto
-- extension so SHA / encryption helpers are available if needed.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
