package com.vissr.safetyscore.data.datasource.remote.retrofit

data class ATRequest(
    val action: String,
    val agToken: String,
    val purpose: String,
    val pop: String
)
