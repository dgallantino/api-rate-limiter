-- KEYS[1] counter hash
-- ARGV: now_ms, window_ms, limit, cost (0 = peek)
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])

local exists = redis.call('EXISTS', KEYS[1]) == 1
local cs, cc, pc = 0, 0, 0
if exists then
  local d = redis.call('HMGET', KEYS[1], 'cs', 'cc', 'pc')
  cs = tonumber(d[1]) or 0
  cc = tonumber(d[2]) or 0
  pc = tonumber(d[3]) or 0
end

local ws = now - (now % window)
if exists then
  if ws == cs + window then
    pc = cc
    cc = 0
    cs = ws
  elseif ws ~= cs then
    pc = 0
    cc = 0
    cs = ws
  end
else
  cs = ws
end

local elapsed = now - cs
if elapsed < 0 then elapsed = 0 end
if elapsed > window then elapsed = window end

local used = pc * (1.0 - (elapsed / window)) + cc
local rem = math.floor(limit - used)
if rem < 0 then rem = 0 end
local retry = math.floor(window - elapsed)
if retry < 0 then retry = 0 end

if cost == 0 then
  local allowed = 0
  if rem >= 1 then allowed = 1 end
  if allowed == 1 then retry = 0 end
  return {allowed, rem, retry}
end

if cost > limit then
  return {0, rem, 0}
end

if used + cost <= limit then
  cc = cc + cost
  used = pc * (1.0 - (elapsed / window)) + cc
  rem = math.floor(limit - used)
  if rem < 0 then rem = 0 end
  redis.call('HSET', KEYS[1], 'cs', cs, 'cc', cc, 'pc', pc)
  redis.call('PEXPIRE', KEYS[1], math.floor(window * 2))
  return {1, rem, 0}
end

return {0, rem, retry}
