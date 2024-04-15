package com.vissr.safetyscore.data.datasource.remote.retrofit

data class AGTRequest(
    val action: String,
    val vin: String,
    val context: String,
    val proof: String,
    val key: String
)
