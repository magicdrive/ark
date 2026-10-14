function a() {}
function b() {}
module.exports = { a, X: 1 }
Object.defineProperty(module.exports, "a", { value: b })
