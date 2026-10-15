function a() {}
function b() {}
module.exports = { a, X: 1 }
delete module.exports["a"]
