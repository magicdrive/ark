<?php
namespace App\Modern;

#[Attribute]
class Tagged {}

readonly class Config
{
    public function __construct(
        public string $env = 'prod',
    ) {}
}

function build(int|string $x, ?Config $c = null): int|null
{
    $fn = fn($v) => $v + 1;
    $obj = new class {
        public function inner(): void {}
    };
    return $fn(1);
}
