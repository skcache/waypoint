-- Runs once when the PostgreSQL container initializes a fresh volume
-- (docker-entrypoint-initdb.d). Creates the separate database used by the
-- integration tests, so a test run can never touch development data.
CREATE DATABASE waypoint_test;
