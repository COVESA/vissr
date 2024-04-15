package com.vissr.safetyscore.ui.model

data class ViewDataPackages(
    val dataPackage: List<ViewDataPackage>
)

data class ViewDataPackage(
    val path: String,
    val name: String,
    val dataPoints: List<ViewDataPoint>
)

data class ViewDataPoint(
    val value: String,
    val timestamp: String,
)
