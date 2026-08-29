<?php

/**
 * Keep the local HTTPS REST endpoint on its own mapped port. WordPress stores
 * the HTTP storefront URL for a browser-friendly demo, which would otherwise
 * redirect a valid HTTPS API request to the HTTP port.
 */
add_filter(
    'redirect_canonical',
    static function ($redirect, $requested) {
        return is_string($requested) && strpos($requested, '/wp-json/') !== false ? false : $redirect;
    },
    10,
    2
);
