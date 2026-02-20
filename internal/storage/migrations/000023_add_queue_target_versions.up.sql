-- Add target_versions column to update_queue table
-- Stores JSON map of container name -> target version
ALTER TABLE update_queue ADD COLUMN target_versions TEXT NOT NULL DEFAULT '{}';
