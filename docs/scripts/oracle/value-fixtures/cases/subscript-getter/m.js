function a() {}
function b() {}
module.exports = { a, X: 1 }
module.exports["__defineGetter__"]("a", () => b)
