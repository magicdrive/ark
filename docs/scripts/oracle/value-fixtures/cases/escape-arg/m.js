function a() {}
function b() {}
module.exports = { a, X: 1 }
function g(p, k) { p[k] = 1 }
g(module.exports, "a")
