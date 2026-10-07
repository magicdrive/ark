<?php

namespace App\Http;

use App\Services\LoginScreenPolicy;

// PHP 7.4 typed property: the call resolves exactly (a graph edge).
class TypedController
{
    private LoginScreenPolicy $policy;

    public function __construct(LoginScreenPolicy $policy)
    {
        $this->policy = $policy;
    }

    public function show()
    {
        return $this->policy->showsSsoButton();
    }
}
