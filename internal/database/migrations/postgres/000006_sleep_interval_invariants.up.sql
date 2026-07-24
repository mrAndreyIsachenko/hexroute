CREATE UNIQUE INDEX sleep_intervals_one_open_per_node_idx
    ON sleep_intervals (node_id)
    WHERE ended_at IS NULL;

CREATE UNIQUE INDEX sleep_intervals_start_event_idx
    ON sleep_intervals (start_event_id)
    WHERE start_event_id IS NOT NULL;

CREATE UNIQUE INDEX sleep_intervals_end_event_idx
    ON sleep_intervals (end_event_id)
    WHERE end_event_id IS NOT NULL;
