-- Token Bucket Rate Limiter - Atomic Redis Lua Script
-- KEYS[1] = rate limit key (e.g. "rl:user:123")
-- ARGV[1] = bucket capacity (burst)
-- ARGV[2] = refill rate (tokens per second)
-- ARGV[3] = current timestamp (float seconds)
-- ARGV[4] = tokens requested
--
-- Returns: {allowed(0|1), remaining, limit, reset_at, retry_after}
--
-- All state stored in a Redis hash:
--   tokens   = current token count (float)
--   last_ts  = last refill timestamp (float)

local key       = KEYS[1]
local capacity  = tonumber(ARGV[1])
local rate      = tonumber(ARGV[2])
local now       = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

-- Fetch existing bucket state
local bucket = redis.call("HMGET", key, "tokens", "last_ts")
local tokens  = tonumber(bucket[1])
local last_ts = tonumber(bucket[2])

-- Initialize bucket on first request
if tokens == nil then
  tokens  = capacity
  last_ts = now
end

-- Refill tokens based on elapsed time
local elapsed = math.max(0, now - last_ts)
tokens = math.min(capacity, tokens + (elapsed * rate))
last_ts = now

-- Attempt to consume tokens
local allowed = 0
local retry_after = 0.0

if tokens >= requested then
  tokens = tokens - requested
  allowed = 1
else
  -- Calculate how long until enough tokens are available
  local deficit = requested - tokens
  retry_after = deficit / rate
end

-- Compute reset_at: time when bucket would be full again
local reset_at = now
if tokens < capacity then
  reset_at = now + ((capacity - tokens) / rate)
end

-- Persist state with TTL = time to full refill + buffer
local ttl = math.ceil((capacity / rate) + 60)
redis.call("HSET", key, "tokens", tostring(tokens), "last_ts", tostring(last_ts))
redis.call("EXPIRE", key, ttl)

-- Return: allowed, remaining (floor), limit, reset_at (ceil), retry_after
return {
  allowed,
  math.floor(tokens),
  capacity,
  math.ceil(reset_at),
  tostring(retry_after)   -- return as string to preserve decimal
}