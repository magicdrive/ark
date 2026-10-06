<?php
namespace App\Http;

use App\Services\Contract as Agreement;
use App\Services\Foo as Bar;
use App\Services\LogsActivity as Logs;

class C
{
    public function a() { return Bar::make(); }

    public function b(Bar $f) { return $f->run(); }

    public function c() { return new Bar(); }
}

class D extends Bar
{
}

class E implements Agreement
{
    public function run() {}
}

class F
{
    use Logs;
}
