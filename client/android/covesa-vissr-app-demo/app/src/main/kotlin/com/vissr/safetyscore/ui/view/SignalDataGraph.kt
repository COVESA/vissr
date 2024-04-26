package com.vissr.safetyscore.ui.view

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign

@Composable
fun CurveLoggingDataGraph() {
    Text(
        modifier = Modifier.fillMaxSize(),
        textAlign = TextAlign.Center,
        text = "Curve Logging"
    )
}
