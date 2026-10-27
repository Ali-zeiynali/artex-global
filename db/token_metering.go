package db

// Input includes cached tokens; cache read/write are subsets, not additional
// input. Use one source per exploration so result summaries cannot duplicate
// the always-on call ledger. Old imported tasks may have results only.
const explorationTokenRows = `
SELECT exploration_id,input_tokens,output_tokens,
       cache_read AS cache_read_tokens,cache_write AS cache_write_tokens
FROM llm_usage WHERE exploration_id IS NOT NULL
UNION ALL
SELECT a.exploration_id,a.input_tokens,a.output_tokens,a.cache_read_tokens,a.cache_write_tokens
FROM activity a
WHERE a.kind='result' AND NOT EXISTS (
    SELECT 1 FROM llm_usage u WHERE u.exploration_id=a.exploration_id
)`
