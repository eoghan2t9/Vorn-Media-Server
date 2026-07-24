DROP TABLE content_request_fulfillments;
DROP INDEX idx_libraries_one_default_target;
ALTER TABLE libraries DROP COLUMN default_request_target;
