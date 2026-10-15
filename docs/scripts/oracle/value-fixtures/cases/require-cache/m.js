function a() {}
function b() {}
module.exports = { a, X: 1 }
require.cache[__filename].exports = { a: b }
