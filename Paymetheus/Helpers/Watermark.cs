using System.Windows.Controls;
using System.Windows.Media;

namespace Paymetheus.Helpers
{
    public class Watermark
    {
        public static void Set(TextBox target, string watermark)
        {
            var foreground = target.Foreground;
            AddWaterMark(target, watermark);
            target.GotFocus += (sender, e) =>
            {
                RemoveWaterMark(target, watermark, foreground);
            };
            target.LostFocus += (sender, e) =>
            {
                AddWaterMark(target, watermark);
            };
        }

        private static void AddWaterMark(TextBox target, string watermark)
        {
            if (target.Text == "")
            {
                target.Foreground = new SolidColorBrush(Colors.Gray);
                target.Text = watermark;
            }
        }

        private static void RemoveWaterMark(TextBox target, string watermark, Brush foreground)
        {
            if (target.Text == watermark)
            {
                target.Text = "";
                target.Foreground = foreground;
            }
        }
    }
}
