package com.vissr.safetyscore.utils.mapper

interface Mapper<T, R> {

    fun map(value: T): R

    fun map(values: List<T>): List<R> = values.map(this::map)

    fun reverse(value: R): T {
        throw UnsupportedOperationException()
    }
}
