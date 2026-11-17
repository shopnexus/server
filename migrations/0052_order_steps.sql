-- A step of an order names its order, and so does a notice about one, so a
-- tap on a notice about an earlier order of a deal opens that order.
--
-- A step of an order happens at one of the order's own times, written in
-- the same save from the same clock reading: the request at requested_at,
-- the shipment at shipped_at, its end (completed, failed, cancelled) at
-- closed_at, and a carrier's update while it is shipped. A step from before
-- this file takes the one order of its deal that matches it. A step that
-- matches no order, or two, keeps a NULL order_id. No CHECK requires the
-- column: the release before writes steps without it while a deploy rolls
-- out, and a Restate invocation pinned to that release retries its save
-- until it lands.

ALTER TABLE deal_moves ADD COLUMN order_id uuid REFERENCES orders (id);

WITH matched AS (
    SELECT
        m.id,
        (array_agg(o.id))[1] AS order_id
    FROM deal_moves AS m
    INNER JOIN orders AS o
        ON
            m.deal_id = o.deal_id
            AND CASE m.kind
                WHEN 'cod_requested' THEN m.moved_at = o.requested_at
                WHEN 'shipped' THEN m.moved_at = o.shipped_at
                WHEN 'shipment_updated'
                    THEN m.moved_at >= o.shipped_at AND m.moved_at <= coalesce(o.closed_at, 'infinity')
                ELSE m.moved_at = o.closed_at
            END
    WHERE m.kind IN ('cod_requested', 'shipped', 'shipment_updated', 'completed', 'failed', 'order_cancelled')
    GROUP BY m.id
    HAVING count(*) = 1
)

UPDATE deal_moves AS m
SET order_id = matched.order_id
FROM matched
WHERE m.id = matched.id;

-- An order notice keeps the params of the step it last told of, written at
-- that step's time (bumped_at), and its type is that step's kind. It takes
-- the order of the one step of its deal with that kind and time. A notice
-- without such a step names no order and opens its deal.
WITH matched AS (
    SELECT
        n.id,
        (array_agg(m.order_id))[1] AS order_id
    FROM notifications AS n
    INNER JOIN deal_moves AS m
        ON
            (n.params ->> 'deal_id')::uuid = m.deal_id
            AND n.notice_type = m.kind
            AND n.bumped_at = m.moved_at
    WHERE
        n.notice_type IN ('cod_requested', 'shipped', 'completed', 'failed', 'order_cancelled')
        AND m.order_id IS NOT NULL
    GROUP BY n.id
    HAVING count(DISTINCT m.order_id) = 1
)

UPDATE notifications AS n
SET params = jsonb_set(n.params, '{order_id}', to_jsonb(matched.order_id::text))
FROM matched
WHERE n.id = matched.id;
