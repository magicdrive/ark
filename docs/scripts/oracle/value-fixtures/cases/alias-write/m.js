function a() {}
function b() {}
module.exports = { a, X: 1 }
const alias = module.exports
alias.a = b
