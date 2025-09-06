CREATE FUNCTION get_unique_prefix(change_id BIGINT)
RETURNS TEXT AS $$
DECLARE
    target_name TEXT;
    target_repo_id INTEGER;
    max_prefix_len INTEGER := 0;
    other_name TEXT;
    common_len INTEGER;
BEGIN
    -- Get the target change
    SELECT name, repository_id INTO target_name, target_repo_id
    FROM changes
    WHERE id = change_id;
    
    IF target_name IS NULL THEN
        RETURN NULL;
    END IF;
    
    -- Find the longest common prefix with any other change
    FOR other_name IN 
        SELECT name 
        FROM changes 
        WHERE repository_id = target_repo_id 
        AND id != change_id
        AND name IS NOT NULL
    LOOP
        -- Calculate common prefix length
        common_len := 0;
        FOR i IN 1..LEAST(LENGTH(target_name), LENGTH(other_name)) LOOP
            IF SUBSTRING(target_name, i, 1) = SUBSTRING(other_name, i, 1) THEN
                common_len := i;
            ELSE
                EXIT;
            END IF;
        END LOOP;
        
        max_prefix_len := GREATEST(max_prefix_len, common_len);
    END LOOP;
    
    -- Return prefix that's 1 character longer than longest common prefix
    RETURN SUBSTRING(target_name FROM 1 FOR GREATEST(1, max_prefix_len + 1));
END;
$$ LANGUAGE plpgsql;
