<?php
namespace App\Support;

trait LogsActivity
{
    protected array $log = [];

    public function record(string $msg): void
    {
        $this->log[] = $msg;
    }
}
