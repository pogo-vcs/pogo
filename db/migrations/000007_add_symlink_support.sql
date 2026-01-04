-- Add symlink support to files table
-- symlink_target stores the target path for symbolic links
-- When non-NULL, the file is treated as a symlink
ALTER TABLE files ADD COLUMN symlink_target TEXT;
